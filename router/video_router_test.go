package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPublicVideoContentRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, ok := routes[http.MethodGet+" /public/videos/:public_task_id/content"]
	require.True(t, ok)
	_, ok = routes[http.MethodGet+" /public/images/assets/:asset_id"]
	require.True(t, ok)
}
