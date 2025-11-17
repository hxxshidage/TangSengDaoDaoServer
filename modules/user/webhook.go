package user

import (
	"fmt"
	common2 "github.com/TangSengDaoDao/TangSengDaoDaoServer/modules/common"
	rd "github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/redis"
	"github.com/go-redis/redis"

	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/common"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/config"
	"go.uber.org/zap"
)

// 处理上下线通知
func (u *User) handleOnlineStatus(onlineStatuses []config.OnlineStatus) {

	u.Debug("收到在线通知")

	if onlineStatuses == nil || len(onlineStatuses) <= 0 {
		return
	}

	for _, onlineStatus := range onlineStatuses {
		if u.ctx.GetConfig().IsVisitor(onlineStatus.UID) { // 如果是访客不做处理
			continue
		}
		if !onlineStatus.Online && onlineStatus.OnlineCount > 0 { // 如果DeviceFlag下还有其他设备在线，则不做离线逻辑处理
			continue
		}
		mainFlag := u.getMainDeviceFlag()                               // 客户端显示的设备名
		isMain := false                                                 // 是否需要设置主设备
		allOffline := false                                             // 是否所有都设备都离线了
		if onlineStatus.Online && mainFlag == onlineStatus.DeviceFlag { // 当前上线的用户为主设备
			isMain = true
		}
		if !isMain {
			mainDeviceFlagM, err := u.getOnlineMainDeviceFlagModel(onlineStatus) // 获取最优显示的主设备
			if err != nil {
				u.Error("判断是否需要将在线状态推送给好友失败！", zap.Error(err), zap.String("uid", onlineStatus.UID))
				continue
			}
			if mainDeviceFlagM != nil {
				mainFlag = mainDeviceFlagM.DeviceFlag
			} else {
				allOffline = true
				mainFlag = onlineStatus.DeviceFlag
			}

		}

		friendUids, err := u.getFriendUidsAndSetCache(onlineStatus.UID)
		if err != nil {
			u.Error("获取好友uid集合失败！", zap.Error(err))
			return
		}

		if len(friendUids) > 0 {
			var online int
			if onlineStatus.Online {
				online = 1
			}
			if onlineStatus.DeviceFlag != config.APP.Uint8() { // 如果是pc端或web端，则通知到自己的其他设备
				friendUids = append(friendUids, onlineStatus.UID) // 如果是pc端或web端在线，则消息也推送给在线者的其他设备
			}
			param := map[string]interface{}{
				"online":      online,
				"device_flag": onlineStatus.DeviceFlag,
				"uid":         onlineStatus.UID,
			}
			param["main_device_flag"] = mainFlag
			if allOffline {
				param["all_offline"] = 1
			}
			err = u.ctx.SendCMD(config.MsgCMDReq{
				Subscribers: friendUids,
				CMD:         common.CMDOnlineStatus,
				NoPersist:   true,
				Param:       param,
			})
			if err != nil {
				u.Warn("发送在线状态cmd失败！", zap.Error(err))
				continue
			}
		}
	}

}

func (u *User) handleOnlineStatusV1(onlineStatuses []config.OnlineStatus) {
	// redis中维护在线/离线 状态
	if len(onlineStatuses) == 0 {
		return
	}

	uidToOnline := make(map[string]bool)

	for _, item := range onlineStatuses {
		uid := item.UID
		isWeb := config.DeviceFlag(item.DeviceFlag) == config.Web

		// uid去重, 对于web客户端来说同一个uid online优先级高于offline
		if isWeb {
			if item.Online {
				uidToOnline[uid] = true
			}
		} else {
			// 非 Web：仅当未被 Web 设为 online 时，才更新状态
			if !uidToOnline[uid] {
				uidToOnline[uid] = item.Online
			}
		}
	}

	bool2uids := make(map[bool][]any)
	for uid, online := range uidToOnline {
		bool2uids[online] = append(bool2uids[online], uid)
	}

	u.Debug("handle online uids group by results", zap.Any("onlineUids", bool2uids[true]), zap.Any("offlineUids", bool2uids[false]))

	cli := rd.GetRedisCli()

	var addCmd, remCmd *redis.IntCmd

	_, err := cli.Pipelined(func(pl redis.Pipeliner) error {
		if len(bool2uids[true]) > 0 {
			addCmd = pl.SAdd(common2.UserOnlineKey, bool2uids[true]...)
		}

		if len(bool2uids[false]) > 0 {
			remCmd = pl.SRem(common2.UserOnlineKey, bool2uids[false]...)
		}

		return nil
	})

	if err != nil {
		u.Error("handle online uids pipeline exec failed", zap.Error(err))
		return
	}

	var addCount, remCount int64
	// 获取返回值
	if addCmd != nil {
		addCount, err = addCmd.Result()
		if err != nil {
			addCount = -1
		}
	}

	if remCmd != nil {
		remCount, err = remCmd.Result()
		if err != nil {
			remCount = -1
		}
	}

	u.Info("handle online uids pipeline exec success", zap.Int64("addCount", addCount), zap.Int64("remCount", remCount))
}

// 获取在线的主设备
func (u *User) getOnlineMainDeviceFlagModel(onlineStatus config.OnlineStatus) (*onlineStatusModel, error) {

	onlineMaxWeightStatus, err := u.onlineDB.queryOnlineMaxWeightWithUID(onlineStatus.UID)
	if err != nil {
		u.Error("获取在线设备里最大权重的设备失败！", zap.Error(err), zap.String("uid", onlineStatus.UID))
		return nil, err
	}
	if onlineMaxWeightStatus != nil {
		return onlineMaxWeightStatus, nil
	}
	return nil, nil

}

// 获取好友uid 并缓存
func (u *User) getFriendUidsAndSetCache(uid string) ([]string, error) {
	friendKey := fmt.Sprintf("%s%s", CacheKeyFriends, uid)
	members, err := u.ctx.GetRedisConn().SMembers(friendKey)
	if err != nil {
		return nil, err
	}
	if len(members) <= 0 {
		friendModels, err := u.friendDB.QueryFriends(uid)
		if err != nil {
			return nil, err
		}
		if len(friendModels) > 0 {
			members = make([]string, 0, len(friendModels))
			memberObjs := make([]interface{}, 0, len(friendModels))
			for _, friendModel := range friendModels {
				memberObjs = append(memberObjs, friendModel.ToUID)
				members = append(members, friendModel.ToUID)
			}
			err = u.ctx.GetRedisConn().SAdd(friendKey, memberObjs...)
			if err != nil {
				return nil, err
			}
		}
	}
	return members, nil

}

// 获取主设备标记
func (u *User) getMainDeviceFlag() uint8 {
	deviceFlagModels, err := u.getDeviceFlags()
	if err != nil {
		return uint8(config.APP)
	}
	var mainDeviceFlagM *deviceFlagModel
	for _, deviceFlagM := range deviceFlagModels {
		if mainDeviceFlagM == nil {
			mainDeviceFlagM = deviceFlagM
			continue
		}
		if deviceFlagM.Weight > mainDeviceFlagM.Weight {
			mainDeviceFlagM = deviceFlagM
		}

	}
	if mainDeviceFlagM == nil {
		return config.Web.Uint8()
	}
	return mainDeviceFlagM.DeviceFlag

}

func (u *User) getDeviceFlags() ([]*deviceFlagModel, error) {
	if u.deviceFlagsCache == nil {
		var err error
		u.deviceFlagsCache, err = u.deviceFlagDB.queryAll()
		if err != nil {
			return nil, err
		}
		if u.deviceFlagsCache == nil {
			u.deviceFlagsCache = make([]*deviceFlagModel, 0)
		}
	}
	return u.deviceFlagsCache, nil
}
