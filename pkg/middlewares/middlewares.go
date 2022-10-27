package middlewares

import (
	"fmt"
	"github.com/gin-gonic/gin"
)

func CorsEnabledMiddleware(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "http://localhost:3000")
	fmt.Println("triggered")
	c.Next()
}
