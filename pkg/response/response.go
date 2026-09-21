// Package response 提供统一的 HTTP JSON 响应格式（code/msg/data）。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Success 返回成功响应，code 固定为 0
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
}

// Fail 返回失败响应，业务码与 HTTP 状态码保持一致
func Fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg, "data": nil})
}
