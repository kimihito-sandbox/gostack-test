package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
)

func main() {
	e := echo.New()
	e.Logger.SetLevel(log.INFO)

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
		return c.String(http.StatusOK, "Hello, World!")
	})

	e.Logger.Fatal(e.Start(":8080"))
}
