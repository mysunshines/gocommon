package response

import (
	"net/http"

	"github.com/mysunshines/gocommon/constants"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type PageResult struct {
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Data     interface{} `json:"data"`
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

func TooManyRequests(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusTooManyRequests, constants.ErrCodeRateLimited, message)
}

func ParamError(c *gin.Context, message string) {
	ErrorWithStatus(c, http.StatusBadRequest, constants.ErrCodeBadRequest, message)
}

func UserNotFound(c *gin.Context) {
	Error(c, constants.ErrCodeUserNotFound, "User not found")
}

func ArticleNotFound(c *gin.Context) {
	Error(c, constants.ErrCodeArticleNotFound, "Article not found")
}

func CommentNotFound(c *gin.Context) {
	Error(c, constants.ErrCodeCommentNotFound, "Comment not found")
}

func PasswordIncorrect(c *gin.Context) {
	Error(c, constants.ErrCodePasswordIncorrect, "Password incorrect")
}

func UserExists(c *gin.Context) {
	Error(c, constants.ErrCodeUserExists, "User already exists")
}

func InvalidToken(c *gin.Context) {
	Error(c, constants.ErrCodeTokenInvalid, "Invalid token")
}

func TokenExpired(c *gin.Context) {
	Error(c, constants.ErrCodeTokenExpired, "Token expired")
}

func PermissionDenied(c *gin.Context) {
	Error(c, constants.ErrCodeForbidden, "Permission denied")
}

func CommentDisabled(c *gin.Context) {
	Error(c, constants.ErrCodeCommentDisabled, "Comment is disabled for this article")
}

func InBlacklist(c *gin.Context) {
	Error(c, constants.ErrCodeCommentBlacklist, "You are in blacklist")
}
