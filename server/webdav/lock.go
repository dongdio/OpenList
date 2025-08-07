// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdav

import (
	"container/heap"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dongdio/OpenList/v4/utility/errs"
)

// WebDAV锁定系统相关错误
var (
	// ErrConfirmationFailed 当锁定确认失败时由LockSystem的Confirm方法返回
	ErrConfirmationFailed = errs.New("webdav: 锁定确认失败")

	// ErrForbidden 当无权限解锁时由LockSystem的Unlock方法返回
	ErrForbidden = errs.New("webdav: 禁止访问")

	// ErrLocked 当资源已被锁定时由LockSystem的Create、Refresh和Unlock方法返回
	ErrLocked = errs.New("webdav: 资源已锁定")

	// ErrNoSuchLock 当指定的锁不存在时由LockSystem的Refresh和Unlock方法返回
	ErrNoSuchLock = errs.New("webdav: 锁不存在")
)

// Condition 表示WebDAV资源的匹配条件，基于令牌或ETag
// Token和ETag中应该有且仅有一个非空
type Condition struct {
	Not   bool   // 是否为否定条件
	Token string // 锁定令牌
	ETag  string // 实体标签
}

// LockSystem 管理对命名资源集合的访问
// 锁名称中的元素由斜杠('/', U+002F)字符分隔，与主机操作系统约定无关
type LockSystem interface {
	// Confirm 确认调用方可以声明指定条件的所有锁，
	// 并且持有这些锁的并集可以对所有指定资源进行独占访问。
	// 最多可以命名两个资源，空名称将被忽略。
	//
	// release和err中有且仅有一个非nil。如果release非nil，
	// 则所有请求的锁将被持有直到调用release。调用release不会
	// 在WebDAV UNLOCK的意义上解锁，但一旦Confirm确认锁定声明有效，
	// 该锁就不能再被Confirm，直到它被释放。
	//
	// 如果Confirm返回ErrConfirmationFailed，则Handler将继续尝试
	// 请求中提供的其他锁集合（WebDAV HTTP请求可以提供多个锁集合）。
	// 如果返回任何其他非nil错误，Handler将写入"500 Internal Server Error"HTTP状态。
	Confirm(now time.Time, name0, name1 string, conditions ...Condition) (release func(), err error)

	// Create 使用给定的深度、持续时间、所有者和根(name)创建锁。
	// 深度要么为负(表示无限)，要么为零。
	//
	// 如果Create返回ErrLocked，则Handler将写入"423 Locked"HTTP状态。
	// 如果返回任何其他非nil错误，Handler将写入"500 Internal Server Error"HTTP状态。
	//
	// 有关何时使用每种错误的信息，请参见
	// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.10.6
	//
	// 返回的令牌标识创建的锁。它应该是由RFC 3986第4.3节定义的绝对URI。
	// 特别是，它不应包含空格。
	Create(now time.Time, details LockDetails) (token string, err error)

	// Refresh 刷新具有给定令牌的锁的持续时间。
	//
	// 如果Refresh返回ErrLocked，则Handler将写入"423 Locked"HTTP状态。
	// 如果Refresh返回ErrNoSuchLock，则Handler将写入"412 Precondition Failed"HTTP状态。
	// 如果返回任何其他非nil错误，Handler将写入"500 Internal Server Error"HTTP状态。
	//
	// 有关何时使用每种错误的信息，请参见
	// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.10.6
	Refresh(now time.Time, token string, duration time.Duration) (LockDetails, error)

	// Unlock 解锁具有给定令牌的锁。
	//
	// 如果Unlock返回ErrForbidden，则Handler将写入"403 Forbidden"HTTP状态。
	// 如果Unlock返回ErrLocked，则Handler将写入"423 Locked"HTTP状态。
	// 如果Unlock返回ErrNoSuchLock，则Handler将写入"409 Conflict"HTTP状态。
	// 如果返回任何其他非nil错误，Handler将写入"500 Internal Server Error"HTTP状态。
	//
	// 有关何时使用每种错误的信息，请参见
	// http://www.webdav.org/specs/rfc4918.html#rfc.section.9.11.1
	Unlock(now time.Time, token string) error
}

// LockDetails 表示锁的元数据信息
type LockDetails struct {
	// Root 是被锁定的根资源名称
	// 对于零深度锁，根是唯一被锁定的资源
	Root string

	// Duration 是锁的超时时间
	// 负持续时间表示无限期
	Duration time.Duration

	// OwnerXML 是LOCK HTTP请求中给出的原始<owner>XML内容
	//
	// TODO: "原始"内容是否与XML命名空间兼容？
	// OwnerXML字段是否需要更多结构？参见
	// https://codereview.appspot.com/175140043/#msg2
	OwnerXML string

	// ZeroDepth 表示锁是否具有零深度
	// 如果没有零深度，则具有无限深度
	ZeroDepth bool
}

// NewMemLS 返回一个新的内存中的LockSystem实现
func NewMemLS() LockSystem {
	return &memLS{
		byName:  make(map[string]*memLSNode),
		byToken: make(map[string]*memLSNode),
		gen:     uint64(time.Now().Unix()),
	}
}

type memLS struct {
	mu      sync.Mutex            // 保护以下字段的互斥锁
	byName  map[string]*memLSNode // 按资源名称索引的锁节点
	byToken map[string]*memLSNode // 按令牌索引的锁节点
	gen     uint64                // 令牌生成计数器
	// byExpiry 仅包含那些具有有限持续时间且尚未过期的节点
	byExpiry byExpiry
}

// nextToken 生成一个新的唯一令牌
// 使用基于时间的计数器确保唯一性
func (m *memLS) nextToken() string {
	m.gen++
	return strconv.FormatUint(m.gen, 10)
}

// collectExpiredNodes 收集并移除所有已过期的锁节点
// 此方法应在持有m.mu锁的情况下调用
//
// 参数:
//   - now: 当前时间，用于判断节点是否过期
func (m *memLS) collectExpiredNodes(now time.Time) {
	for len(m.byExpiry) > 0 {
		// 如果堆顶节点未过期，则所有节点都未过期，退出循环
		if now.Before(m.byExpiry[0].expiry) {
			break
		}
		// 移除过期节点
		m.remove(m.byExpiry[0])
	}
}

// Confirm 实现LockSystem接口的Confirm方法
// 确认调用方可以声明指定条件的所有锁
//
// 参数:
//   - now: 当前时间
//   - name0, name1: 要确认的资源名称（最多两个）
//   - conditions: 锁条件列表
//
// 返回:
//   - func(): 释放函数，调用后释放持有的锁
//   - error: 错误信息
func (m *memLS) Confirm(now time.Time, name0, name1 string, conditions ...Condition) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 首先清理过期节点
	m.collectExpiredNodes(now)

	// 查找并确认每个资源的锁
	var n0, n1 *memLSNode
	if name0 != "" {
		// 清理路径并查找匹配条件的锁
		if n0 = m.lookup(slashClean(name0), conditions...); n0 == nil {
			return nil, ErrConfirmationFailed
		}
	}
	if name1 != "" {
		// 清理路径并查找匹配条件的锁
		if n1 = m.lookup(slashClean(name1), conditions...); n1 == nil {
			return nil, ErrConfirmationFailed
		}
	}

	// 避免持有同一个节点两次
	if n1 == n0 {
		n1 = nil
	}

	// 持有找到的节点
	if n0 != nil {
		m.hold(n0)
	}
	if n1 != nil {
		m.hold(n1)
	}

	// 返回释放函数
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if n1 != nil {
			m.unhold(n1)
		}
		if n0 != nil {
			m.unhold(n0)
		}
	}, nil
}

// lookup 返回锁定指定资源的节点n
// 前提是n至少匹配给定条件之一，并且该锁未被其他方持有
// 如果n是无限深度锁，则n可能是指定资源的父节点
//
// 参数:
//   - name: 资源名称
//   - conditions: 要匹配的条件列表
//
// 返回:
//   - *memLSNode: 匹配的锁节点，如果没有匹配则返回nil
func (m *memLS) lookup(name string, conditions ...Condition) (n *memLSNode) {
	// TODO: 支持Condition.Not和Condition.ETag
	for _, c := range conditions {
		// 通过令牌查找节点
		n = m.byToken[c.Token]
		// 如果节点不存在或已被持有，则继续检查下一个条件
		if n == nil || n.held {
			continue
		}
		// 如果资源名称与锁的根资源完全匹配
		if name == n.details.Root {
			return n
		}
		// 如果节点是无限深度锁，则继续检查下一个条件
		if n.details.ZeroDepth {
			continue
		}
		// 如果资源名称是根资源或其子资源
		if n.details.Root == "/" || strings.HasPrefix(name, n.details.Root+"/") {
			return n
		}
	}
	return nil
}

func (m *memLS) hold(n *memLSNode) {
	if n.held {
		panic("webdav: memLS inconsistent held state")
	}
	n.held = true
	if n.details.Duration >= 0 && n.byExpiryIndex >= 0 {
		heap.Remove(&m.byExpiry, n.byExpiryIndex)
	}
}

func (m *memLS) unhold(n *memLSNode) {
	if !n.held {
		panic("webdav: memLS inconsistent held state")
	}
	n.held = false
	if n.details.Duration >= 0 {
		heap.Push(&m.byExpiry, n)
	}
}

func (m *memLS) Create(now time.Time, details LockDetails) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectExpiredNodes(now)
	details.Root = slashClean(details.Root)

	if !m.canCreate(details.Root, details.ZeroDepth) {
		return "", ErrLocked
	}
	n := m.create(details.Root)
	n.token = m.nextToken()
	m.byToken[n.token] = n
	n.details = details
	if n.details.Duration >= 0 {
		n.expiry = now.Add(n.details.Duration)
		heap.Push(&m.byExpiry, n)
	}
	return n.token, nil
}

func (m *memLS) Refresh(now time.Time, token string, duration time.Duration) (LockDetails, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectExpiredNodes(now)

	n := m.byToken[token]
	if n == nil {
		return LockDetails{}, ErrNoSuchLock
	}
	if n.held {
		return LockDetails{}, ErrLocked
	}
	if n.byExpiryIndex >= 0 {
		heap.Remove(&m.byExpiry, n.byExpiryIndex)
	}
	n.details.Duration = duration
	if n.details.Duration >= 0 {
		n.expiry = now.Add(n.details.Duration)
		heap.Push(&m.byExpiry, n)
	}
	return n.details, nil
}

func (m *memLS) Unlock(now time.Time, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectExpiredNodes(now)

	n := m.byToken[token]
	if n == nil {
		return ErrNoSuchLock
	}
	if n.held {
		return ErrLocked
	}
	m.remove(n)
	return nil
}

func (m *memLS) canCreate(name string, zeroDepth bool) bool {
	return walkToRoot(name, func(name0 string, first bool) bool {
		n := m.byName[name0]
		if n == nil {
			return true
		}
		if first {
			if n.token != "" {
				// The target node is already locked.
				return false
			}
			if !zeroDepth {
				// The requested lock depth is infinite, and the fact that n exists
				// (n != nil) means that a descendent of the target node is locked.
				return false
			}
		} else if n.token != "" && !n.details.ZeroDepth {
			// An ancestor of the target node is locked with infinite depth.
			return false
		}
		return true
	})
}

func (m *memLS) create(name string) (ret *memLSNode) {
	walkToRoot(name, func(name0 string, first bool) bool {
		n := m.byName[name0]
		if n == nil {
			n = &memLSNode{
				details: LockDetails{
					Root: name0,
				},
				byExpiryIndex: -1,
			}
			m.byName[name0] = n
		}
		n.refCount++
		if first {
			ret = n
		}
		return true
	})
	return ret
}

func (m *memLS) remove(n *memLSNode) {
	delete(m.byToken, n.token)
	n.token = ""
	walkToRoot(n.details.Root, func(name0 string, first bool) bool {
		x := m.byName[name0]
		x.refCount--
		if x.refCount == 0 {
			delete(m.byName, name0)
		}
		return true
	})
	if n.byExpiryIndex >= 0 {
		heap.Remove(&m.byExpiry, n.byExpiryIndex)
	}
}

func walkToRoot(name string, f func(name0 string, first bool) bool) bool {
	for first := true; ; first = false {
		if !f(name, first) {
			return false
		}
		if name == "/" {
			break
		}
		name = name[:strings.LastIndex(name, "/")]
		if name == "" {
			name = "/"
		}
	}
	return true
}

type memLSNode struct {
	// details are the lock metadata. Even if this node's name is not explicitly locked,
	// details.Root will still equal the node's name.
	details LockDetails
	// token is the unique identifier for this node's lock. An empty token means that
	// this node is not explicitly locked.
	token string
	// refCount is the number of self-or-descendent nodes that are explicitly locked.
	refCount int
	// expiry is when this node's lock expires.
	expiry time.Time
	// byExpiryIndex is the index of this node in memLS.byExpiry. It is -1
	// if this node does not expire, or has expired.
	byExpiryIndex int
	// held is whether this node's lock is actively held by a Confirm call.
	held bool
}

type byExpiry []*memLSNode

func (b *byExpiry) Len() int {
	return len(*b)
}

func (b *byExpiry) Less(i, j int) bool {
	return (*b)[i].expiry.Before((*b)[j].expiry)
}

func (b *byExpiry) Swap(i, j int) {
	(*b)[i], (*b)[j] = (*b)[j], (*b)[i]
	(*b)[i].byExpiryIndex = i
	(*b)[j].byExpiryIndex = j
}

func (b *byExpiry) Push(x any) {
	n := x.(*memLSNode)
	n.byExpiryIndex = len(*b)
	*b = append(*b, n)
}

func (b *byExpiry) Pop() any {
	i := len(*b) - 1
	n := (*b)[i]
	(*b)[i] = nil
	n.byExpiryIndex = -1
	*b = (*b)[:i]
	return n
}

const infiniteTimeout = -1

// parseTimeout parses the Timeout HTTP header, as per section 10.7. If s is
// empty, an infiniteTimeout is returned.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return infiniteTimeout, nil
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "Infinite" {
		return infiniteTimeout, nil
	}
	const pre = "Second-"
	if !strings.HasPrefix(s, pre) {
		return 0, errInvalidTimeout
	}
	s = s[len(pre):]
	if s == "" || s[0] < '0' || '9' < s[0] {
		return 0, errInvalidTimeout
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || 1<<32-1 < n {
		return 0, errInvalidTimeout
	}
	return time.Duration(n) * time.Second, nil
}