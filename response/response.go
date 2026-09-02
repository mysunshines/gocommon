package response

import (
	"net/http"

	"github.com/mysunshines/gocommon/constants"

	"github.com/gin-gonic/gin"
)

// Response 统一API响应结构
type Response struct {
	Code    int         `json:"code"`         // 业务码：0 表示成功，非 0 表示错误
	Message string      `json:"message"`      // 提示信息
	Data    interface{} `json:"data,omitempty"` // 业务数据（成功时返回，可为空）
}

// PageResult 分页结果
type PageResult struct {
	Total    int64       `json:"total"`    // 总记录数
	Page     int         `json:"page"`     // 当前页
	PageSize int         `json:"page_size"`// 每页大小
	Data     interface{} `json:"data"`     // 当前页数据列表
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: message,
		Data:    data,
	})
}

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

func Error(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: message,
	})
}

func ErrorWithStatus(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
	})
}

func BadRequest(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusBadRequest, constants.ErrCodeBadRequest, message)
}

func Unauthorized(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusUnauthorized, constants.ErrCodeUnauthorized, message)
}

func Forbidden(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusForbidden, constants.ErrCodeForbidden, message)
}

func NotFound(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusNotFound, constants.ErrCodeNotFound, message)
}

func InternalServerError(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusInternalServerError, constants.ErrCodeInternal, message)
}

// Fail 统一返回 500 及错误详情，便于 handler 中直接透传 err（兼容既有调用约定）。
func Fail(c *gin.Context, err error) {
	InternalServerError(c, err.Error())
}

func TooManyRequests(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusTooManyRequests, constants.ErrCodeRateLimited, message)
}

func ParamError(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusBadRequest, constants.ErrCodeBadRequest, message)
}

func PermissionDenied(c *gin.Context) {
	Error(c, constants.ErrCodeForbidden, "Permission denied")
}
