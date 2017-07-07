package kvdb

import (
	"time"

	"github.com/xiaonanln/goSyncQueue"
	"github.com/xiaonanln/goTimer"
	"github.com/xiaonanln/goworld/config"
	"github.com/xiaonanln/goworld/gwlog"
	"github.com/xiaonanln/goworld/kvdb/backend/mongodb"
	. "github.com/xiaonanln/goworld/kvdb/types"
	"github.com/xiaonanln/goworld/netutil"
	"github.com/xiaonanln/goworld/opmon"
)

var (
	kvdbEngine  KVDBEngine
	kvdbOpQueue sync_queue.SyncQueue
)

type KVDBEngine interface {
	Get(key string) (val string, err error)
	Put(key string, val string) (err error)
	Find(key string) Iterator
}

type KVDBGetCallback func(val string, err error)
type KVDBPutCallback func(err error)
type KVDBGetRangeCallback func(items []KVItem, err error)

func Initialize() {
	var err error
	kvdbCfg := config.GetKVDB()
	if kvdbCfg.Type == "" {
		return // kvdb not enabled
	}

	if kvdbCfg.Type == "mongodb" {
		kvdbEngine, err = kvdb_mongo.OpenMongoKVDB(kvdbCfg.Url, kvdbCfg.DB, kvdbCfg.Collection)
		if err != nil {
			gwlog.Panic(err)
		}
	}

	kvdbOpQueue = sync_queue.NewSyncQueue()
	go netutil.ServeForever(kvdbRoutine)
}

type getReq struct {
	key      string
	callback KVDBGetCallback
}

type putReq struct {
	key      string
	val      string
	callback KVDBPutCallback
}

type getRangeReq struct {
	beginKey string
	endKey   string
	callback KVDBGetRangeCallback
}

func Get(key string, callback KVDBGetCallback) {
	kvdbOpQueue.Push(&getReq{
		key, callback,
	})
	checkOperationQueueLen()
}

func Put(key string, val string, callback KVDBPutCallback) {
	kvdbOpQueue.Push(&putReq{
		key, val, callback,
	})
	checkOperationQueueLen()
}

func GetRange(beginKey string, endKey string, callback KVDBGetRangeCallback) {
	kvdbOpQueue.Push(&getRangeReq{
		beginKey, endKey, callback,
	})
	checkOperationQueueLen()
}

var recentWarnedQueueLen = 0

func checkOperationQueueLen() {
	qlen := kvdbOpQueue.Len()
	if qlen > 100 && qlen%100 == 0 && recentWarnedQueueLen != qlen {
		gwlog.Warn("KVDB operation queue length = %d", qlen)
		recentWarnedQueueLen = qlen
	}
}

func kvdbRoutine() {
	for {
		req := kvdbOpQueue.Pop()
		var op *opmon.Operation
		if getReq, ok := req.(*getReq); ok {
			op = opmon.StartOperation("kvdb.get")
			handleGetReq(getReq)
		} else if putReq, ok := req.(*putReq); ok {
			op = opmon.StartOperation("kvdb.put")
			handlePutReq(putReq)
		} else if getRangeReq, ok := req.(*getRangeReq); ok {
			op = opmon.StartOperation("kvdb.getRange")
			handleGetRangeReq(getRangeReq)
		}
		op.Finish(time.Millisecond * 100)
	}
}

func handleGetReq(getReq *getReq) {
	val, err := kvdbEngine.Get(getReq.key)
	if getReq.callback != nil {
		timer.AddCallback(0, func() {
			getReq.callback(val, err)
		})
	}
}

func handlePutReq(putReq *putReq) {
	err := kvdbEngine.Put(putReq.key, putReq.val)
	if putReq.callback != nil {
		timer.AddCallback(0, func() {
			putReq.callback(err)
		})
	}
}

func handleGetRangeReq(getRangeReq *getRangeReq) {
	it := kvdbEngine.Find(getRangeReq.beginKey)
	var items []KVItem
	endKey := getRangeReq.endKey
	for {
		item, err := it.Next()
		if item.Key >= endKey {
			// it is the end, end is not included
			break
		}

		if err != nil {
			if getRangeReq.callback != nil {
				getRangeReq.callback(nil, err)
			}
			return
		}

		items = append(items, item)
	}

	if getRangeReq.callback != nil {
		getRangeReq.callback(items, nil)
	}
}