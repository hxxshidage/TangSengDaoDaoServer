package user

import (
	"github.com/TangSengDaoDao/TangSengDaoDaoServer/modules/common"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/log"
	rd "github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/redis"
	"go.uber.org/zap"
	"sync/atomic"
)

var (
	nq      chan string
	started atomic.Bool
)

func init() {
	nq = make(chan string, 50)
}

func StartCleanCache() {
	if !started.CompareAndSwap(false, true) {
		return
	}

	for {
		for uid := range nq {
			delUserCache(uid)
		}
	}
}

func delUserCache(uid string) {
	cli := rd.GetRedisCli()
	if _, err := cli.Del(common.UserCachePrefix + uid).Result(); err != nil {
		log.Warn("delete user cache in redis failed, uid:"+uid, zap.Error(err))
	}
}

func OnProfileModify(uid string) {
	select {
	case nq <- uid:
	default:
		// do with sync
		delUserCache(uid)
	}
}
