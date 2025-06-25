// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdav

// XML 编码相关实现，遵循 WebDAV 规范第 14 节
// http://www.webdav.org/specs/rfc4918.html#xml.element.definitions

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"

	// 由于 Go 1.5 中对命名空间编码方式的变更（后来在 Go 1.6 中被回滚），
	// 本包使用了标准库 encoding/xml 的内部分支。
	// 这是因为 https://github.com/golang/go/issues/11841 问题的存在。
	//
	// 然而，本包的导出 API（特别是 Property 和 DeadPropsHolder 类型）
	// 需要引用标准库版本的 xml.Name 类型，因为导入本包的代码无法引用内部版本。
	//
	// 因此，本文件同时导入内部版本和外部版本，分别命名为 ixml 和 xml，并在它们之间进行转换。
	//
	// 长期来看，一旦 https://github.com/golang/go/issues/13400 得到解决，
	// 本包应该只使用标准库版本，并删除内部分支。
	ixml "github.com/dongdio/OpenList/server/webdav/internal/xml"
)

// lockInfo 表示锁定信息
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_lockinfo
type lockInfo struct {
	XMLName   ixml.Name `xml:"lockinfo"`
	Exclusive *struct{} `xml:"lockscope>exclusive"` // 独占锁
	Shared    *struct{} `xml:"lockscope>shared"`    // 共享锁
	Write     *struct{} `xml:"locktype>write"`      // 写锁
	Owner     owner     `xml:"owner"`               // 锁所有者
}

// owner 表示锁的所有者
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_owner
type owner struct {
	InnerXML string `xml:",innerxml"` // 所有者的 XML 表示
}

// readLockInfo 从请求体中读取锁定信息
// 返回锁定信息、HTTP 状态码和错误
func readLockInfo(r io.Reader) (li lockInfo, status int, err error) {
	c := &countingReader{r: r}
	if err = ixml.NewDecoder(c).Decode(&li); err != nil {
		if err == io.EOF {
			if c.n == 0 {
				// 空请求体表示刷新锁
				// http://www.webdav.org/specs/rfc4918.html#refreshing-locks
				return lockInfo{}, 0, nil
			}
			err = errInvalidLockInfo
		}
		return lockInfo{}, http.StatusBadRequest, err
	}

	// 我们只支持独占（非共享）写锁
	// 实际上，这些是唯一重要的锁类型
	if li.Exclusive == nil || li.Shared != nil || li.Write == nil {
		return lockInfo{}, http.StatusNotImplemented, errUnsupportedLockInfo
	}
	return li, 0, nil
}

// countingReader 是一个计数读取器，记录读取的字节数
type countingReader struct {
	n int       // 已读取的字节数
	r io.Reader // 底层读取器
}

// Read 实现 io.Reader 接口
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// writeLockInfo 将锁信息写入响应
// 返回写入的字节数和错误
func writeLockInfo(w io.Writer, token string, ld LockDetails) (int, error) {
	depth := "infinity"
	if ld.ZeroDepth {
		depth = "0"
	}
	timeout := ld.Duration / time.Second

	// 生成锁信息的 XML 表示
	return fmt.Fprintf(w, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"+
		"<D:prop xmlns:D=\"DAV:\"><D:lockdiscovery><D:activelock>\n"+
		"	<D:locktype><D:write/></D:locktype>\n"+
		"	<D:lockscope><D:exclusive/></D:lockscope>\n"+
		"	<D:depth>%s</D:depth>\n"+
		"	<D:owner>%s</D:owner>\n"+
		"	<D:timeout>Second-%d</D:timeout>\n"+
		"	<D:locktoken><D:href>%s</D:href></D:locktoken>\n"+
		"	<D:lockroot><D:href>%s</D:href></D:lockroot>\n"+
		"</D:activelock></D:lockdiscovery></D:prop>",
		depth, ld.OwnerXML, timeout, escape(token), escape(ld.Root),
	)
}

// escape 对字符串进行 XML 转义
func escape(s string) string {
	// 检查是否需要转义
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '&', '\'', '<', '>':
			b := bytes.NewBuffer(nil)
			ixml.EscapeText(b, []byte(s))
			return b.String()
		}
	}
	return s
}

// next 返回 XML 流中的下一个标记（如果有）
// RFC 4918 要求忽略注释、处理指令和指令
// http://www.webdav.org/specs/rfc4918.html#property_values
// http://www.webdav.org/specs/rfc4918.html#xml-extensibility
func next(d *ixml.Decoder) (ixml.Token, error) {
	for {
		t, err := d.Token()
		if err != nil {
			return t, err
		}
		switch t.(type) {
		case ixml.Comment, ixml.Directive, ixml.ProcInst:
			// 跳过这些标记类型
			continue
		default:
			return t, nil
		}
	}
}

// propfindProps 表示 PROPFIND 请求中的属性列表
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_prop (for propfind)
type propfindProps []xml.Name

// UnmarshalXML 将 start 元素中包含的属性名称追加到 pn
//
// 如果 start 不包含任何属性或者属性包含值，则返回错误
// 属性之间的字符数据将被忽略
func (pn *propfindProps) UnmarshalXML(d *ixml.Decoder, start ixml.StartElement) error {
	for {
		t, err := next(d)
		if err != nil {
			return err
		}
		switch t.(type) {
		case ixml.EndElement:
			if len(*pn) == 0 {
				return errors.Errorf("%s 不能为空", start.Name.Local)
			}
			return nil
		case ixml.StartElement:
			name := t.(ixml.StartElement).Name
			t, err = next(d)
			if err != nil {
				return err
			}
			if _, ok := t.(ixml.EndElement); !ok {
				return errors.Errorf("意外的标记 %T", t)
			}
			*pn = append(*pn, xml.Name(name))
		}
	}
}

// propfind 表示 PROPFIND 请求
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propfind
type propfind struct {
	XMLName  ixml.Name     `xml:"DAV: propfind"`
	Allprop  *struct{}     `xml:"DAV: allprop"`  // 请求所有属性
	Propname *struct{}     `xml:"DAV: propname"` // 仅请求属性名
	Prop     propfindProps `xml:"DAV: prop"`     // 请求特定属性
	Include  propfindProps `xml:"DAV: include"`  // 包含的属性
}

// readPropfind 从请求体中读取 PROPFIND 请求
// 返回 propfind 结构、HTTP 状态码和错误
func readPropfind(r io.Reader) (pf propfind, status int, err error) {
	c := countingReader{r: r}
	if err = ixml.NewDecoder(&c).Decode(&pf); err != nil {
		if err == io.EOF {
			if c.n == 0 {
				// 空请求体表示 allprop
				// http://www.webdav.org/specs/rfc4918.html#METHOD_PROPFIND
				return propfind{Allprop: new(struct{})}, 0, nil
			}
			err = errInvalidPropfind
		}
		return propfind{}, http.StatusBadRequest, err
	}

	// 验证请求的有效性
	if pf.Allprop == nil && pf.Include != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Allprop != nil && (pf.Prop != nil || pf.Propname != nil) {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Prop != nil && pf.Propname != nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	if pf.Propname == nil && pf.Allprop == nil && pf.Prop == nil {
		return propfind{}, http.StatusBadRequest, errInvalidPropfind
	}
	return pf, 0, nil
}

// Property 表示 RFC 4918 中定义的单个 DAV 资源属性
// 参见 http://www.webdav.org/specs/rfc4918.html#data.model.for.resource.properties
type Property struct {
	// XMLName 是标识此属性的完全限定名称
	XMLName xml.Name

	// Lang 是可选的 xml:lang 属性
	Lang string `xml:"xml:lang,attr,omitempty"`

	// InnerXML 包含属性值的 XML 表示
	// 参见 http://www.webdav.org/specs/rfc4918.html#property_values
	//
	// 复杂类型或混合内容的属性值必须具有完全展开的 XML 命名空间
	// 或者是带有相应 XML 命名空间声明的自包含内容。它们不能依赖于
	// XML 文档范围内的任何 XML 命名空间声明，甚至包括 DAV: 命名空间。
	InnerXML []byte `xml:",innerxml"`
}

// ixmlProperty 与 Property 类型相同，但它持有 ixml.Name 而不是 xml.Name
type ixmlProperty struct {
	XMLName  ixml.Name
	Lang     string `xml:"xml:lang,attr,omitempty"`
	InnerXML []byte `xml:",innerxml"`
}

// xmlError 表示 XML 错误
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_error
// 参见 multistatusWriter 了解 "D:" 命名空间前缀
type xmlError struct {
	XMLName  ixml.Name `xml:"D:error"`
	InnerXML []byte    `xml:",innerxml"`
}

// propstat 表示属性状态
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propstat
// 参见 multistatusWriter 了解 "D:" 命名空间前缀
type propstat struct {
	Prop                []Property `xml:"D:prop>_ignored_"`                // 属性列表
	Status              string     `xml:"D:status"`                        // 状态码
	Error               *xmlError  `xml:"D:error"`                         // 错误信息
	ResponseDescription string     `xml:"D:responsedescription,omitempty"` // 响应描述
}

// ixmlPropstat 与 propstat 类型相同，但它持有 ixml.Name 而不是 xml.Name
type ixmlPropstat struct {
	Prop                []ixmlProperty `xml:"D:prop>_ignored_"`
	Status              string         `xml:"D:status"`
	Error               *xmlError      `xml:"D:error"`
	ResponseDescription string         `xml:"D:responsedescription,omitempty"`
}

// MarshalXML 在编码前为 DAV: 命名空间中的属性添加 "D:" 命名空间前缀
// 参见 multistatusWriter
func (ps propstat) MarshalXML(e *ixml.Encoder, start ixml.StartElement) error {
	// 将 propstat 转换为 ixmlPropstat
	ixmlPs := ixmlPropstat{
		Prop:                make([]ixmlProperty, len(ps.Prop)),
		Status:              ps.Status,
		Error:               ps.Error,
		ResponseDescription: ps.ResponseDescription,
	}
	for k, prop := range ps.Prop {
		ixmlPs.Prop[k] = ixmlProperty{
			XMLName:  ixml.Name(prop.XMLName),
			Lang:     prop.Lang,
			InnerXML: prop.InnerXML,
		}
	}

	// 为 DAV: 命名空间中的属性添加 "D:" 前缀
	for k, prop := range ixmlPs.Prop {
		if prop.XMLName.Space == "DAV:" {
			prop.XMLName = ixml.Name{Space: "", Local: "D:" + prop.XMLName.Local}
			ixmlPs.Prop[k] = prop
		}
	}
	// 使用不同的类型避免 MarshalXML 的无限递归
	type newpropstat ixmlPropstat
	return e.EncodeElement(newpropstat(ixmlPs), start)
}

// response 表示 WebDAV 响应
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_response
// 参见 multistatusWriter 了解 "D:" 命名空间前缀
type response struct {
	XMLName             ixml.Name  `xml:"D:response"`
	Href                []string   `xml:"D:href"`                          // 资源 URL
	Propstat            []propstat `xml:"D:propstat"`                      // 属性状态
	Status              string     `xml:"D:status,omitempty"`              // 状态码
	Error               *xmlError  `xml:"D:error"`                         // 错误信息
	ResponseDescription string     `xml:"D:responsedescription,omitempty"` // 响应描述
}

// MultistatusWriter 将一个或多个 Response 编组为 XML 多状态响应
// 参见 http://www.webdav.org/specs/rfc4918.html#ELEMENT_multistatus
// 注意：作为一种解决方法，"D:" 命名空间前缀（在此元素上定义为 "DAV:"）
// 也被添加到嵌套的 response 元素及其所有嵌套元素上。DAV: 命名空间中的
// 所有属性名称也都加上了前缀。这是因为某些版本的 Mini-Redirector（在 Windows 7 上）
// 忽略具有默认命名空间（无前缀命名空间）的元素。
type multistatusWriter struct {
	// ResponseDescription 包含多状态 XML 元素的可选 responsedescription
	// 只有关闭前的最新内容才会被发送。空的响应描述不会被写入。
	responseDescription string

	w   http.ResponseWriter // 底层 HTTP 响应写入器
	enc *ixml.Encoder       // XML 编码器
}

// Write 验证并发送 DAV 响应作为多状态响应元素的一部分
//
// 它将底层 http.ResponseWriter 的 HTTP 状态码设置为 207（多状态）
// 并填充 Content-Type 标头。如果 r 是要写入的第一个有效响应，
// Write 会在 r 的 XML 表示前添加一个多状态标签。调用者必须在
// 最后一个响应写入后调用 close。
func (w *multistatusWriter) write(r *response) error {
	// 验证响应的有效性
	switch len(r.Href) {
	case 0:
		return errInvalidResponse
	case 1:
		if len(r.Propstat) > 0 != (r.Status == "") {
			return errInvalidResponse
		}
	default:
		if len(r.Propstat) > 0 || r.Status == "" {
			return errInvalidResponse
		}
	}

	// 写入头部信息
	err := w.writeHeader()
	if err != nil {
		return err
	}

	// 编码响应
	return w.enc.Encode(r)
}

// writeHeader 在 w 的底层 http.ResponseWriter 上写入 XML 多状态开始元素
// 并返回写操作的结果。在第一次写入尝试后，writeHeader 变为空操作。
func (w *multistatusWriter) writeHeader() error {
	// 如果编码器已存在，则不需要再次写入头部
	if w.enc != nil {
		return nil
	}

	// 设置 HTTP 头部和状态码
	w.w.Header().Add("Content-Type", "text/xml; charset=utf-8")
	w.w.WriteHeader(StatusMulti)

	// 写入 XML 声明
	_, err := fmt.Fprintf(w.w, `<?xml version="1.0" encoding="UTF-8"?>`)
	if err != nil {
		return err
	}

	// 创建 XML 编码器
	w.enc = ixml.NewEncoder(w.w)

	// 写入多状态开始标签
	return w.enc.EncodeToken(ixml.StartElement{
		Name: ixml.Name{
			Space: "DAV:",
			Local: "multistatus",
		},
		Attr: []ixml.Attr{{
			Name:  ixml.Name{Space: "xmlns", Local: "D"},
			Value: "DAV:",
		}},
	})
}

// Close 完成多状态响应的编组。如果多状态响应无法完成，则返回错误。
// 如果 w 的返回值和字段 enc 都为 nil，则表示尚未写入多状态响应。
func (w *multistatusWriter) close() error {
	if w.enc == nil {
		return nil
	}

	// 准备结束标记
	var end []ixml.Token

	// 如果有响应描述，则添加
	if w.responseDescription != "" {
		name := ixml.Name{Space: "DAV:", Local: "responsedescription"}
		end = append(end,
			ixml.StartElement{Name: name},
			ixml.CharData(w.responseDescription),
			ixml.EndElement{Name: name},
		)
	}

	// 添加多状态结束标记
	end = append(end, ixml.EndElement{
		Name: ixml.Name{Space: "DAV:", Local: "multistatus"},
	})

	// 编码所有结束标记
	for _, t := range end {
		err := w.enc.EncodeToken(t)
		if err != nil {
			return err
		}
	}

	// 刷新编码器
	return w.enc.Flush()
}

// XML 语言名称常量
var xmlLangName = ixml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "lang"}

// xmlLang 获取 XML 元素的语言属性值
// 如果未找到，则返回默认值 d
func xmlLang(s ixml.StartElement, d string) string {
	for _, attr := range s.Attr {
		if attr.Name == xmlLangName {
			return attr.Value
		}
	}
	return d
}

// xmlValue 表示 XML 值
type xmlValue []byte

// UnmarshalXML 将属性的 XML 值解组
// 属性的 XML 值可以是任意的混合内容 XML
// 为了确保解组后的值包含所有必需的命名空间，
// 我们将所有属性值 XML 标记编码到缓冲区中
// 这迫使编码器重新声明任何使用的命名空间
func (v *xmlValue) UnmarshalXML(d *ixml.Decoder, start ixml.StartElement) error {
	var b bytes.Buffer
	e := ixml.NewEncoder(&b)

	for {
		t, err := next(d)
		if err != nil {
			return err
		}
		if e, ok := t.(ixml.EndElement); ok && e.Name == start.Name {
			break
		}
		if err = e.EncodeToken(t); err != nil {
			return err
		}
	}

	err := e.Flush()
	if err != nil {
		return err
	}

	*v = b.Bytes()
	return nil
}

// proppatchProps 表示 PROPPATCH 请求中的属性列表
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_prop (for proppatch)
type proppatchProps []Property

// UnmarshalXML 将 start 元素中包含的属性名称和值追加到 ps
//
// 在 DAV:prop 或属性名称 XML 元素上定义的 xml:lang 属性
// 将传播到属性的 Lang 字段
//
// 如果 start 不包含任何属性或者属性值包含语法不正确的 XML，则返回错误
func (ps *proppatchProps) UnmarshalXML(d *ixml.Decoder, start ixml.StartElement) error {
	lang := xmlLang(start, "")
	for {
		t, err := next(d)
		if err != nil {
			return err
		}
		switch elem := t.(type) {
		case ixml.EndElement:
			if len(*ps) == 0 {
				return errors.Errorf("%s 不能为空", start.Name.Local)
			}
			return nil
		case ixml.StartElement:
			p := Property{
				XMLName: xml.Name(t.(ixml.StartElement).Name),
				Lang:    xmlLang(t.(ixml.StartElement), lang),
			}
			err = d.DecodeElement(((*xmlValue)(&p.InnerXML)), &elem)
			if err != nil {
				return err
			}
			*ps = append(*ps, p)
		}
	}
}

// setRemove 表示属性设置或移除操作
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_set
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_remove
type setRemove struct {
	XMLName ixml.Name      // 操作类型（set 或 remove）
	Lang    string         `xml:"xml:lang,attr,omitempty"` // 语言
	Prop    proppatchProps `xml:"DAV: prop"`               // 属性列表
}

// propertyupdate 表示属性更新请求
// http://www.webdav.org/specs/rfc4918.html#ELEMENT_propertyupdate
type propertyupdate struct {
	XMLName   ixml.Name   `xml:"DAV: propertyupdate"`
	Lang      string      `xml:"xml:lang,attr,omitempty"` // 语言
	SetRemove []setRemove `xml:",any"`                    // 设置或移除操作列表
}

// readProppatch 从请求体中读取 PROPPATCH 请求
// 返回补丁列表、HTTP 状态码和错误
func readProppatch(r io.Reader) (patches []Proppatch, status int, err error) {
	var pu propertyupdate
	if err = ixml.NewDecoder(r).Decode(&pu); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// 预分配结果切片
	patches = make([]Proppatch, 0, len(pu.SetRemove))

	// 处理每个设置或移除操作
	for _, op := range pu.SetRemove {
		remove := false
		switch op.XMLName {
		case ixml.Name{Space: "DAV:", Local: "set"}:
			// 设置操作，无需特殊处理
		case ixml.Name{Space: "DAV:", Local: "remove"}:
			// 移除操作，验证属性值为空
			for _, p := range op.Prop {
				if len(p.InnerXML) > 0 {
					return nil, http.StatusBadRequest, errInvalidProppatch
				}
			}
			remove = true
		default:
			// 不支持的操作类型
			return nil, http.StatusBadRequest, errInvalidProppatch
		}
		patches = append(patches, Proppatch{Remove: remove, Props: op.Prop})
	}

	return patches, 0, nil
}