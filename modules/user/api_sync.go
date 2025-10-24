package user

import (
	"database/sql"
	"fmt"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/config"
	rd "github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/redis"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/util"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/wkhttp"
	"github.com/alphadose/haxmap"
	"github.com/go-redis/redis"
	"github.com/gocraft/dbr/v2"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

var (
	mgr = func() *syncManager {
		return &syncManager{
			mc: haxmap.New[string, *syncState](),
		}
	}()
)

type syncState struct {
	infoMd5 atomic.Value
	imToken atomic.Value
}

type syncManager struct {
	mc *haxmap.Map[string, *syncState]
}

func (sm *syncManager) getState(uid string) *syncState {
	ss, _ := sm.mc.GetOrCompute(uid, func() *syncState {
		s := &syncState{}
		s.infoMd5.Store("")
		s.imToken.Store("")
		return s
	})

	return ss
}

func (sm *syncManager) needModifyInfo(uid, nickname, avatar string) bool {
	ss := sm.getState(uid)
	if info, ok := ss.infoMd5.Load().(string); ok {
		if info == "" {
			return true
		}

		// 计算md5
		return util.Md5(nickname+avatar) != info
	}
	return false
}

func (sm *syncManager) updateInfoMd5(uid, nickname, avatar string) {
	sm.getState(uid).infoMd5.Store(util.Md5(nickname + avatar))
}

func (sm *syncManager) updateImToken(uid, imToken string) {
	sm.getState(uid).imToken.Store(imToken)
}

func (sm *syncManager) needUpdateImToken(uid, imToken string) bool {
	ss := sm.getState(uid)
	if token, ok := ss.imToken.Load().(string); ok {
		if token == "" {
			return true
		}

		return imToken != token
	}
	return false
}

// 这个接口web端, 调用的非常频繁!!!
func (u *User) syncUserInfo(c *wkhttp.Context) {
	var req struct {
		Uid         string             `json:"uid,omitempty"`
		Nickname    string             `json:"nickname,omitempty"`
		Avatar      string             `json:"avatar,omitempty"`
		DeviceFlag  config.DeviceFlag  `json:"device_flag,omitempty"`
		DeviceLevel config.DeviceLevel `json:"device_level,omitempty"`
		ImToken     string             `json:"im_token,omitempty"`
	}

	if err := c.BindJSON(&req); err != nil {
		u.Error("bind json failed", zap.Error(err))
		c.ResponseError(errors.New("bind json failed"))
		return
	}

	// 写入用户信息
	// 无需严格意义上的并发控制
	if mgr.needModifyInfo(req.Uid, req.Nickname, req.Avatar) {
		if _, err := config.DoWithDb(func(sess *dbr.Session) (sql.Result, error) {
			return sess.InsertBySql("insert into user (uid, name, short_no, avatar) values (?, ?, ?, ?) on DUPLICATE key update `name` = values(name), avatar = values(avatar)",
				req.Uid, req.Nickname, util.Ten2Hex(time.Now().UnixNano()), req.Avatar).Exec()
		}); err != nil {
			u.Error(fmt.Sprintf("upsert user failed when sync info, req:%+v", req), zap.Error(err))
			c.ResponseError(errors.New("upsert user failed"))
			return
		}

		mgr.updateInfoMd5(req.Uid, req.Nickname, req.Avatar)
	}

	// 更新imToken
	// 无需严格意义上的并发控制
	if mgr.needUpdateImToken(req.Uid, req.ImToken) {
		if _, err := u.ctx.UpdateIMToken(config.UpdateIMTokenReq{
			UID:         req.Uid,
			Token:       req.ImToken,
			DeviceFlag:  req.DeviceFlag,
			DeviceLevel: req.DeviceLevel,
		}); err != nil {
			if err != nil {
				u.Error("update im token failed when sync info", zap.Error(err))
				c.ResponseError(errors.New("update im token failed"))
				return
			}
		}

		mgr.updateImToken(req.Uid, req.ImToken)
	}

	// 设置用户token
	var (
		apiToken = util.GenerUUID()
		flag     = req.DeviceFlag
	)

	if flag == config.APP {
		// 获取老的token并清除老token数据
		oldToken, err := u.ctx.Cache().Get(fmt.Sprintf("%s%d%s", u.ctx.GetConfig().Cache.UIDTokenCachePrefix, flag, req.Uid))
		if err != nil {
			u.Error("fetch old token failed when sync info", zap.Error(err))
			c.ResponseError(errors.New("fetch old token failed"))
			return
		}

		if oldToken != "" {
			err = u.ctx.Cache().Delete(u.ctx.GetConfig().Cache.TokenCachePrefix + oldToken)
			if err != nil {
				u.Error("clear old token failed when sync info", zap.Error(err))
				c.ResponseError(errors.New("clear old token failed"))
				return
			}
		}
	}

	// 这里可以用pipeline来优化下
	/*if err := u.ctx.Cache().SetAndExpire(
		u.ctx.GetConfig().Cache.TokenCachePrefix+apiToken,
		fmt.Sprintf("%s@%s@%s", req.Uid, req.Nickname, ""),
		u.ctx.GetConfig().Cache.TokenExpire,
	); err != nil {
		u.Error("setting token2info failed when sync info", zap.Error(err))
		c.ResponseError(errors.New("setting token2info failed"))
		return
	}

	if err := u.ctx.Cache().SetAndExpire(
		fmt.Sprintf("%s%d%s", u.ctx.GetConfig().Cache.UIDTokenCachePrefix, flag, req.Uid),
		apiToken,
		u.ctx.GetConfig().Cache.TokenExpire,
	); err != nil {
		u.Error("setting uid2token failed when sync info", zap.Error(err))
		c.ResponseError(errors.New("setting uid2token failed"))
		return
	}*/

	var (
		cli         = rd.GetRedisCli()
		tokenExpire = u.ctx.GetConfig().Cache.TokenExpire
		tokenKey    = u.ctx.GetConfig().Cache.TokenCachePrefix + apiToken
		uidKey      = fmt.Sprintf("%s%d%s", u.ctx.GetConfig().Cache.UIDTokenCachePrefix, flag, req.Uid)
	)

	cmds, err := cli.Pipelined(func(pl redis.Pipeliner) error {
		pl.Set(tokenKey, fmt.Sprintf("%s@%s@%s", req.Uid, req.Nickname, ""), tokenExpire)
		pl.Set(uidKey, apiToken, tokenExpire)
		return nil
	})

	if err != nil {
		u.Error("setting token pipeline exec failed when sync info", zap.Error(err))
		c.ResponseError(errors.New("setting token pipeline exec failed"))
		return
	}

	for i, cmd := range cmds {
		if e := cmd.Err(); e != nil && e != redis.Nil {
			u.Error(fmt.Sprintf("setting token pl cmd failed when sync info, the %d cmd", i), zap.Error(e))
			c.ResponseError(errors.New("setting token pipeline failed"))
			return
		}
	}

	c.RespWithData(map[string]any{
		"api_token": apiToken,
	})
}

func (u *User) modifyProfile(c *wkhttp.Context) {
	var req struct {
		Uid      string
		Nickname string
		Avatar   string
	}

	// 数据库更新资料
	rst, err := config.DoWithDb(func(sess *dbr.Session) (sql.Result, error) {
		return sess.Update("user").SetMap(map[string]interface{}{
			"name":   req.Nickname,
			"avatar": req.Avatar,
		}).Where("uid = ?", req.Uid).Exec()
	})

	if err == nil {
		if rows, _ := rst.RowsAffected(); rows == 1 {

			// 删除redis缓存
		}
	}

	c.ResponseOK()
}
