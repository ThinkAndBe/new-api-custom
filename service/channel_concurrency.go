package service

import (
	"sync"
)

// channelSem 每渠道的并发信号量。
// limit=0 表示不限流，直接放行。
type channelSem struct {
	ch    chan struct{}
	limit int
}

var (
	channelSemaphores sync.Map // channelId (int) -> *channelSem
)

// getOrCreateSemaphore 获取或创建渠道信号量。
// 如果 limit <= 0 返回 nil（不限流）。
// 如果已存在的信号量 limit 与当前 limit 不同，会重建信号量。
func getOrCreateSemaphore(channelId int, limit int) *channelSem {
	if limit <= 0 {
		return nil
	}
	if v, ok := channelSemaphores.Load(channelId); ok {
		sem := v.(*channelSem)
		// limit 变化时重建（用新容量替换旧的）
		if sem.limit != limit {
			newSem := &channelSem{ch: make(chan struct{}, limit), limit: limit}
			actual, _ := channelSemaphores.LoadOrStore(channelId, newSem)
			return actual.(*channelSem)
		}
		return sem
	}
	newSem := &channelSem{ch: make(chan struct{}, limit), limit: limit}
	actual, _ := channelSemaphores.LoadOrStore(channelId, newSem)
	return actual.(*channelSem)
}

// TryAcquireChannel 尝试获取渠道并发令牌。
// 返回 acquired=true 表示成功获取（调用方必须在请求结束后调用 ReleaseChannel）。
// 返回 acquired=false 表示该渠道已满，应跳过该渠道。
// limit=0 时总是返回 true 且不消耗令牌（无需 Release）。
func TryAcquireChannel(channelId int, limit int) (acquired bool) {
	if limit <= 0 {
		return true
	}
	sem := getOrCreateSemaphore(channelId, limit)
	if sem == nil {
		return true
	}
	select {
	case sem.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// ReleaseChannel 释放渠道并发令牌。
// limit=0 时是 no-op。
func ReleaseChannel(channelId int, limit int) {
	if limit <= 0 {
		return
	}
	if v, ok := channelSemaphores.Load(channelId); ok {
		sem := v.(*channelSem)
		select {
		case <-sem.ch:
		default:
			// 防御性：信号量已空（不应发生），忽略
		}
	}
}

// GetChannelInflight 获取渠道当前在途请求数（用于调试/展示）。
func GetChannelInflight(channelId int) int {
	if v, ok := channelSemaphores.Load(channelId); ok {
		sem := v.(*channelSem)
		return len(sem.ch)
	}
	return 0
}

// CleanupChannelSemaphores 清理已删除渠道的信号量。
func CleanupChannelSemaphores(activeChannelIds map[int]bool) {
	channelSemaphores.Range(func(key, value any) bool {
		id := key.(int)
		if !activeChannelIds[id] {
			channelSemaphores.Delete(id)
		}
		return true
	})
}
