package local

import "context"

// TokenBucket 令牌桶接口，用于限制并发操作
type TokenBucket interface {
	// Take 获取一个令牌，返回一个通道，当有令牌可用时会收到信号
	Take() <-chan struct{}
	// Put 归还一个令牌到桶中
	Put()
	// Do 使用令牌执行函数，自动获取和归还令牌
	Do(context.Context, func() error) error
}

// StaticTokenBucket 固定大小的令牌桶实现
// 初始状态下桶是满的，令牌的获取和归还需要手动控制
type StaticTokenBucket struct {
	bucket chan struct{} // 用于存储令牌的通道
}

// NewStaticTokenBucket 创建一个新的固定大小令牌桶
// size: 桶的容量（最大令牌数）
func NewStaticTokenBucket(size int) StaticTokenBucket {
	if size <= 0 {
		size = 1
	}
	bucket := make(chan struct{}, size)
	// 初始化桶，填满令牌
	for i := 0; i < size; i++ {
		bucket <- struct{}{}
	}
	return StaticTokenBucket{bucket: bucket}
}

// NewStaticTokenBucketWithMigration 创建一个新的令牌桶，并从旧桶迁移令牌
// oldBucket: 旧的令牌桶
// size: 新桶的容量
func NewStaticTokenBucketWithMigration(oldBucket TokenBucket, size int) StaticTokenBucket {
	if size <= 0 {
		size = 1
	}
	// 如果有旧桶，尝试迁移令牌
	if oldBucket == nil {
		// 如果没有旧桶或旧桶类型不匹配，创建新桶
		return NewStaticTokenBucket(size)
	}

	oldStaticBucket, ok := oldBucket.(StaticTokenBucket)
	if !ok {
		// 如果没有旧桶或旧桶类型不匹配，创建新桶
		return NewStaticTokenBucket(size)
	}

	oldSize := cap(oldStaticBucket.bucket)
	// 计算需要迁移的令牌数量
	migrateSize := min(oldSize, size)

	// 创建新桶并预填充部分令牌
	bucket := make(chan struct{}, size)
	for i := 0; i < size-migrateSize; i++ {
		bucket <- struct{}{}
	}

	// 异步迁移剩余令牌
	if migrateSize > 0 {
		go func() {
			for range migrateSize {
				// 从旧桶取出令牌放入新桶
				_, ok := <-oldStaticBucket.bucket
				if !ok {
					// 旧桶已关闭，提前结束迁移
					break
				}
				bucket <- struct{}{}
			}
			// 关闭旧桶
			close(oldStaticBucket.bucket)
		}()
	}
	return StaticTokenBucket{bucket: bucket}
}

// Take 获取一个令牌
// 注意：当驱动被修改时，通道可能会被关闭
// 通道关闭后不应再调用Put方法
func (b StaticTokenBucket) Take() <-chan struct{} {
	return b.bucket
}

// Put 归还一个令牌到桶中
func (b StaticTokenBucket) Put() {
	select {
	case b.bucket <- struct{}{}:
		// 成功归还令牌
	default:
		// 桶已满或已关闭，忽略
	}
}

// Do 使用令牌执行函数
// ctx: 上下文，用于取消操作
// f: 要执行的函数
func (b StaticTokenBucket) Do(ctx context.Context, f func() error) error {
	select {
	case <-ctx.Done():
		// 上下文已取消
		return ctx.Err()
	case _, ok := <-b.Take():
		// 获取到令牌或通道已关闭
		if ok {
			// 获取到令牌，执行完后归还
			defer b.Put()
		}
	}
	// 执行函数
	return f()
}

// NopTokenBucket 无操作令牌桶实现
// 所有操作都会立即成功，不进行实际限流
type NopTokenBucket struct {
	nop chan struct{} // 已关闭的通道，用于立即返回
}

// NewNopTokenBucket 创建一个新的无操作令牌桶
func NewNopTokenBucket() NopTokenBucket {
	nop := make(chan struct{})
	close(nop) // 关闭通道，使Take方法立即返回
	return NopTokenBucket{nop: nop}
}

// Take 立即返回一个已关闭的通道
func (b NopTokenBucket) Take() <-chan struct{} {
	return b.nop
}

// Put 无操作
func (b NopTokenBucket) Put() {
	// 无需实际操作
}

// Do 直接执行函数，不进行限流
func (b NopTokenBucket) Do(_ context.Context, f func() error) error {
	return f()
}
