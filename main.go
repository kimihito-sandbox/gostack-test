package main

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"

	"github.com/kimihito-sandbox/gostack-test/views"
)

func main() {
	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true, // HTTPステータスコードを記録
		LogURI:      true, // リクエストURIを記録
		LogError:    true, // エラー情報を記録
		HandleError: true, // エラー時もログを出力
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				e.Logger.Infof("REQUEST: uri=%v, status=%v", v.URI, v.Status)
			} else {
				e.Logger.Errorf("REQUEST ERROR: uri=%v, status=%v, err=%v", v.URI, v.Status, v.Error)
			}
			return nil
		},
	}))
	e.Use(middleware.Recover())

	e.GET("/", func(c echo.Context) error {
		return render(c, http.StatusOK, views.Hello("World"))
	})

	e.Logger.Fatal(e.Start(":8080"))
}

// render はTemplコンポーネントをEchoのレスポンスとして返すヘルパー関数
func render(c echo.Context, statusCode int, t templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(statusCode)
	return t.Render(c.Request().Context(), c.Response())
}
