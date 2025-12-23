package main

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/aarondl/opt/omit"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/stephenafamo/bob"
	_ "modernc.org/sqlite"

	"github.com/kimihito-sandbox/gostack-test/models"
	"github.com/kimihito-sandbox/gostack-test/views"
)

func main() {
	// DB接続
	sqlDB, err := sql.Open("sqlite", "db/app.db")
	if err != nil {
		panic(err)
	}
	defer sqlDB.Close()
	db := bob.NewDB(sqlDB)

	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	// ミドルウェア
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
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:    "form:csrf_token",       // フォームからトークンを取得
		CookieName:     "_csrf",                 // Cookieの名前
		CookieSecure:   false,                   // 開発環境ではfalse、本番ではtrue
		CookieHTTPOnly: true,                    // JavaScriptからアクセス不可
		CookieSameSite: http.SameSiteStrictMode, // CSRF対策を強化
	}))

	// ルーティング
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/todos")
	})

	// Todo一覧
	e.GET("/todos", func(c echo.Context) error {
		ctx := context.Background()
		todos, err := models.Todos.Query().All(ctx, db)
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoIndex(todos, csrfToken))
	})

	// Todo作成
	e.POST("/todos", func(c echo.Context) error {
		ctx := context.Background()
		title := c.FormValue("title")
		if title == "" {
			return c.Redirect(http.StatusFound, "/todos")
		}
		todo, err := models.Todos.Insert(&models.TodoSetter{
			Title: omit.From(title),
		}).One(ctx, db) // 作成したTodoを取得
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoItem(todo, csrfToken))
	})

	// Todo完了状態の切り替え
	e.POST("/todos/:id/toggle", func(c echo.Context) error {
		ctx := context.Background()
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			return err
		}

		// 現在のTodoを取得
		todo, err := models.Todos.Query(
			models.SelectWhere.Todos.ID.EQ(id),
		).One(ctx, db)
		if err != nil {
			return err
		}

		// 完了状態を反転して更新
		err = todo.Update(ctx, db, &models.TodoSetter{
			Completed: omit.From(!todo.Completed),
		})
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoItem(todo, csrfToken))
	})

	// Todo削除
	e.POST("/todos/:id/delete", func(c echo.Context) error {
		ctx := context.Background()
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			return err
		}
		_, err = models.Todos.Delete(
			models.DeleteWhere.Todos.ID.EQ(id),
		).Exec(ctx, db)
		if err != nil {
			return err
		}
		return c.NoContent(http.StatusOK) // hx-swap="delete"なのでレスポンス不要
	})

	e.Logger.Fatal(e.Start(":8080"))
}

// render はTemplコンポーネントをEchoのレスポンスとして返すヘルパー関数
func render(c echo.Context, statusCode int, t templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(statusCode)
	return t.Render(c.Request().Context(), c.Response())
}

