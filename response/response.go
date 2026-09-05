package response

import (
	"net/http"

	"github.com/mysunshines/gocommon/constants"

	"github.com/gin-gonic/gin"
)

// Response 统一API响应结构
type Response struct {
	Code    int         `json:"code"`           // 业务码：0 表示成功，非 0 表示错误
	Message string      `json:"message"`        // 提示信息
	Data    interface{} `json:"data,omitempty"` // 业务数据（成功时返回，可为空）
}

// PageResult 分页结果
type PageResult struct {
	Total    int64       `json:"total"`     // 总记录数
	Page     int         `json:"page"`      // 当前页
	PageSize int         `json:"page_size"` // 每页大小
	Data     interface{} `json:"data"`      // 当前页数据列表
}

// Success 以 HTTP 200 与业务码 0 返回成功响应，并携带业务数据。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// SuccessWithMessage 以 HTTP 200 与业务码 0 返回成功响应，允许自定义提示信息。
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: message,
		Data:    data,
	})
}

// SuccessPage 以 HTTP 200 返回分页成功响应，封装总数、页码、页大小与当前页数据。
func SuccessPage(c *gin.Context, total int64, page, pageSize int, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data: PageResult{
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			Data:     data,
		},
	})
}

// Error 以 HTTP 200 与指定业务码返回错误响应（HTTP 状态仍为 200，便于前端统一处理）。
func Error(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: message,
	})
}

// ErrorWithStatus 以指定 HTTP 状态码与业务码返回错误响应。
func ErrorWithStatus(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
	})
}

// BadRequest 返回 400 参数错误响应（业务码 ErrCodeBadRequest）。
func BadRequest(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusBadRequest, constants.ErrCodeBadRequest, message)
}

// Unauthorized 返回 401 未授权响应（业务码 ErrCodeUnauthorized）。
func Unauthorized(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusUnauthorized, constants.ErrCodeUnauthorized, message)
}

// Forbidden 返回 403 禁止访问响应（业务码 ErrCodeForbidden）。
func Forbidden(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusForbidden, constants.ErrCodeForbidden, message)
}

// NotFound 返回 404 资源不存在响应（业务码 ErrCodeNotFound）。
func NotFound(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusNotFound, constants.ErrCodeNotFound, message)
}

// InternalServerError 返回 500 服务器内部错误响应（业务码 ErrCodeInternal）。
func InternalServerError(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusInternalServerError, constants.ErrCodeInternal, message)
}

// Fail 统一返回 500 及错误详情，便于 handler 中直接透传 err（兼容既有调用约定）。
func Fail(c *gin.Context, err error) {
	InternalServerError(c, err.Error())
}

// TooManyRequests 返回 429 请求过多（限流）响应（业务码 ErrCodeRateLimited）。
func TooManyRequests(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusTooManyRequests, constants.ErrCodeRateLimited, message)
}

// ParamError 返回 400 参数校验失败响应（业务码 ErrCodeBadRequest）。
func ParamError(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusBadRequest, constants.ErrCodeBadRequest, message)
}

// PermissionDenied 返回业务码 ErrCodeForbidden 的权限不足响应。
func PermissionDenied(c *gin.Context) {
	Error(c, constants.ErrCodeForbidden, "Permission denied")
}
