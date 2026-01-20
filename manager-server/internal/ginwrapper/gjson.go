package ginwrapper

import (
	"encoding/json"
	"net/http"
	"strconv"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	jsoniter "github.com/json-iterator/go"
)

// 定义一个我们配置好的 jsoniter API
var jsonAPI = jsoniter.ConfigCompatibleWithStandardLibrary

// CustomJSONRender 结构体用于替换 Gin 的默认 JSON 渲染器
type CustomJSONRender struct{}

// Render (JSON) 手动实现了 render.Render 接口
func (r CustomJSONRender) Render(w http.ResponseWriter) error {
	// 这里什么都不做，因为 WriteContentType 和 Marshal 会在下面被调用
	return nil
}

// WriteContentType (JSON) 实现了 render.Render 接口
func (r CustomJSONRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = []string{"application/json; charset=utf-8"}
	}
}

// Marshal (JSON) 是自定义渲染器的关键部分，它使用 jsoniter 进行序列化
func (r CustomJSONRender) Marshal(v any) ([]byte, error) {
	return jsonAPI.Marshal(v)
}

// CustomJSONBinding 结构体用于替换 Gin 的默认 JSON 绑定器
type CustomJSONBinding struct{}

// Name 返回绑定器的名字
func (b CustomJSONBinding) Name() string {
	return "json"
}

// Bind (JSON) 实现了 binding.Binding 接口，使用 jsoniter 进行反序列化
func (b CustomJSONBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return &json.SyntaxError{Offset: 0}
	}
	decoder := jsonAPI.NewDecoder(req.Body)
	return decoder.Decode(obj)
}

// BindBody (JSON) 实现了 binding.BindingBody 接口
func (b CustomJSONBinding) BindBody(body []byte, obj any) error {
	return jsonAPI.Unmarshal(body, obj)
}

// init 函数会在包被导入时自动执行
func init() {
	// ===== 注册自定义的编解码器 =====

	// 注册 int64 的编码器：将 int64 转为 string
	jsoniter.RegisterTypeEncoderFunc("int64", func(ptr unsafe.Pointer, stream *jsoniter.Stream) {
		val := *((*int64)(ptr))
		stream.WriteString(strconv.FormatInt(val, 10))
	}, nil)

	// 注册 uint64 的编码器：将 uint64 转为 string
	jsoniter.RegisterTypeEncoderFunc("uint64", func(ptr unsafe.Pointer, stream *jsoniter.Stream) {
		val := *((*uint64)(ptr))
		stream.WriteString(strconv.FormatUint(val, 10))
	}, nil)

	// 注册 int64 的解码器：可以从 string 或 number 类型中解析
	jsoniter.RegisterTypeDecoderFunc("int64", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		var val int64
		var err error
		// 尝试按字符串读取
		if iter.WhatIsNext() == jsoniter.StringValue {
			val, err = strconv.ParseInt(iter.ReadString(), 10, 64)
		} else { // 否则按数字读取
			val = iter.ReadInt64()
		}

		if err != nil {
			iter.Error = err
			return
		}
		*((*int64)(ptr)) = val
	})

	// 注册 uint64 的解码器：可以从 string 或 number 类型中解析
	jsoniter.RegisterTypeDecoderFunc("uint64", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		var val uint64
		var err error
		if iter.WhatIsNext() == jsoniter.StringValue {
			val, err = strconv.ParseUint(iter.ReadString(), 10, 64)
		} else {
			val = iter.ReadUint64()
		}

		if err != nil {
			iter.Error = err
			return
		}
		*((*uint64)(ptr)) = val
	})

	// ===== 替换 Gin 的默认组件 =====

	// 替换 Gin 的 JSON 绑定器
	binding.JSON = CustomJSONBinding{}
	// render.JSON 实例是 Gin 内部用来调用 c.JSON() 的
	// 我们通过创建一个新的实例来覆盖它
	//render.JSON = CustomJSONRender{}
}

// 我们创建一个实现了 render.Render 接口的结构体

// JsoniterRender 包含要被序列化的数据
type JsoniterRender struct {
	Data any
}

func (r JsoniterRender) Render(w http.ResponseWriter) error {
	// 使用我们的 jsoniter API 来 Marshal 数据
	bytes, err := jsonAPI.Marshal(r.Data)
	if err != nil {
		return err
	}
	_, err = w.Write(bytes)
	return err
}

func (r JsoniterRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header.Set("Content-Type", "application/json; charset=utf-8")
	}
}

// JSON 是一个辅助函数，它模拟了 gin.Context.JSON 的行为，但使用 jsoniter 进行渲染
func JSON(c *gin.Context, code int, obj any) {
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Render(code, JsoniterRender{Data: obj})
}
