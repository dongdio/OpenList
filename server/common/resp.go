package common

// Resp 通用响应结构体，用于API返回数据
// 使用泛型支持不同类型的数据
// 参数:
//   - T: 数据类型参数，可以是任意类型
type Resp[T any] struct {
	Code    int    `json:"code"`    // 状态码，200表示成功
	Message string `json:"message"` // 响应消息
	Data    T      `json:"data"`    // 响应数据
}

// PageResp 分页响应结构体，用于返回分页数据
type PageResp struct {
	Content any   `json:"content"` // 分页内容
	Total   int64 `json:"total"`   // 总记录数
}
