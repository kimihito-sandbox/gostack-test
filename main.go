package main

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/aarondl/opt/omit"
	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/olivere/vite"
	"github.com/pressly/goose/v3"
	"github.com/stephenafamo/bob"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	z "github.com/Oudwins/zog"
	"github.com/Oudwins/zog/zhttp"
	"github.com/kimihito-sandbox/gostack-test/models"
	"github.com/kimihito-sandbox/gostack-test/views"
)

//go:embed all:frontend/dist
var distFS embed.FS

// バリデーション用の構造体とスキーマ
type LoginInput struct {
	Email    string `zog:"email"`
	Password string `zog:"password"`
}

var loginSchema = z.Struct(z.Shape{
	"Email":    z.String().Required(z.Message("メールアドレスは必須です")).Email(z.Message("有効なメールアドレスを入力してください")),
	"Password": z.String().Required(z.Message("パスワードは必須です")),
})

type RegisterInput struct {
	Email           string `zog:"email"`
	Password        string `zog:"password"`
	ConfirmPassword string `zog:"confirm_password"`
}

var registerSchema = z.Struct(z.Shape{
	"Email":           z.String().Required(z.Message("メールアドレスは必須です")).Email(z.Message("有効なメールアドレスを入力してください")),
	"Password":        z.String().Required(z.Message("パスワードは必須です")).Min(8, z.Message("パスワードは8文字以上で入力してください")),
	"ConfirmPassword": z.String().Required(z.Message("パスワード確認は必須です")),
})

type TodoInput struct {
	Title string `zog:"title"`
}

var todoSchema = z.Struct(z.Shape{
	"Title": z.String().Required(z.Message("タイトルは必須です")).Min(1, z.Message("タイトルは必須です")),
})

func main() {
	// DB接続
	sqlDB, err := sql.Open("sqlite", "db/app.db")
	if err != nil {
		panic(err)
	}
	defer sqlDB.Close()

	// マイグレーション実行
	if err := goose.SetDialect("sqlite3"); err != nil {
		panic(err)
	}
	if err := goose.Up(sqlDB, "db/migrations"); err != nil {
		panic(err)
	}

	db := bob.NewDB(sqlDB)

	// セッションマネージャーの初期化（SQLiteストア）
	sessionManager := scs.New()
	sessionManager.Store = sqlite3store.New(sqlDB)
	sessionManager.Lifetime = 24 * time.Hour

	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	// Vite設定
	isDev := os.Getenv("VITE_DEV") == "true"
	var viteConfig vite.Config
	if isDev {
		viteConfig = vite.Config{
			FS:           os.DirFS("./frontend"),
			IsDev:        true,
			ViteURL:      "http://localhost:5173",
			ViteEntry:    "src/main.js",
			ViteTemplate: vite.Vanilla,
		}
	} else {
		subFS, err := fs.Sub(distFS, "frontend/dist")
		if err != nil {
			panic(err)
		}
		viteConfig = vite.Config{
			FS:           subFS,
			IsDev:        false,
			ViteTemplate: vite.Vanilla,
		}
	}

	viteFragment, err := vite.HTMLFragment(viteConfig)
	if err != nil {
		panic(err)
	}

	// Viteアセットハンドラー（本番モードのみ）
	if !isDev {
		assetsFS, _ := fs.Sub(distFS, "frontend/dist/assets")
		e.StaticFS("/assets", echo.MustSubFS(assetsFS, "."))
	}

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

	// scsセッションミドルウェア
	e.Use(echo.WrapMiddleware(sessionManager.LoadAndSave))

	// CSRFミドルウェア
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:    "form:csrf_token",       // フォームからトークンを取得
		CookieName:     "_csrf",                 // Cookieの名前
		CookiePath:     "/",                     // Cookie適用パス（全体）
		CookieSecure:   false,                   // 開発環境ではfalse、本番ではtrue
		CookieHTTPOnly: true,                    // JavaScriptからアクセス不可
		CookieSameSite: http.SameSiteStrictMode, // CSRF対策を強化
	}))

	// ViteタグをContextに注入するミドルウェア
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := vite.ScriptsToContext(c.Request().Context(), string(viteFragment.Tags))
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	// ルーティング
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/todos")
	})

	// ========== 認証 ==========

	// ログインページ表示
	e.GET("/auth/login", func(c echo.Context) error {
		// 既にログイン済みならリダイレクト
		if sessionManager.GetInt64(c.Request().Context(), "user_id") != 0 {
			return c.Redirect(http.StatusFound, "/todos")
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.LoginPage(csrfToken, nil))
	})

	// ログイン処理
	e.POST("/auth/login", func(c echo.Context) error {
		ctx := c.Request().Context()
		csrfToken := c.Get("csrf").(string)

		var input LoginInput
		if issues := loginSchema.Parse(zhttp.Request(c.Request()), &input); len(issues) > 0 {
			return render(c, http.StatusBadRequest, views.LoginPage(csrfToken, issuesToMap(issues)))
		}

		// ユーザーを検索
		user, err := models.Users.Query(
			models.SelectWhere.Users.Email.EQ(input.Email),
		).One(ctx, db)
		if err != nil {
			return render(c, http.StatusBadRequest, views.LoginPage(csrfToken, map[string][]string{"_": {"メールアドレスまたはパスワードが正しくありません"}}))
		}

		// パスワード検証
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
			return render(c, http.StatusBadRequest, views.LoginPage(csrfToken, map[string][]string{"_": {"メールアドレスまたはパスワードが正しくありません"}}))
		}

		// セッションにユーザーIDを保存
		sessionManager.Put(ctx, "user_id", user.ID)

		return c.Redirect(http.StatusFound, "/todos")
	})

	// 新規登録ページ表示
	e.GET("/auth/register", func(c echo.Context) error {
		// 既にログイン済みならリダイレクト
		if sessionManager.GetInt64(c.Request().Context(), "user_id") != 0 {
			return c.Redirect(http.StatusFound, "/todos")
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.RegisterPage(csrfToken, nil))
	})

	// 新規登録処理
	e.POST("/auth/register", func(c echo.Context) error {
		ctx := c.Request().Context()
		csrfToken := c.Get("csrf").(string)

		var input RegisterInput
		if issues := registerSchema.Parse(zhttp.Request(c.Request()), &input); len(issues) > 0 {
			return render(c, http.StatusBadRequest, views.RegisterPage(csrfToken, issuesToMap(issues)))
		}

		// パスワード確認チェック
		if input.Password != input.ConfirmPassword {
			return render(c, http.StatusBadRequest, views.RegisterPage(csrfToken, map[string][]string{"confirm_password": {"パスワードが一致しません"}}))
		}

		// 既存ユーザーチェック
		_, err := models.Users.Query(
			models.SelectWhere.Users.Email.EQ(input.Email),
		).One(ctx, db)
		if err == nil {
			return render(c, http.StatusBadRequest, views.RegisterPage(csrfToken, map[string][]string{"email": {"このメールアドレスは既に登録されています"}}))
		}

		// パスワードハッシュ化
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		// ユーザー作成
		now := time.Now()
		user, err := models.Users.Insert(&models.UserSetter{
			Email:     omit.From(input.Email),
			Password:  omit.From(string(hashedPassword)),
			CreatedAt: omit.From(now),
			UpdatedAt: omit.From(now),
		}).One(ctx, db)
		if err != nil {
			return err
		}

		// セッションにユーザーIDを保存（自動ログイン）
		sessionManager.Put(ctx, "user_id", user.ID)

		return c.Redirect(http.StatusFound, "/todos")
	})

	// ログアウト
	e.POST("/auth/logout", func(c echo.Context) error {
		sessionManager.Destroy(c.Request().Context())
		return c.Redirect(http.StatusFound, "/auth/login")
	})

	// ========== Todo（認証必須） ==========

	// 認証が必要なルートグループ
	protected := e.Group("/todos")
	protected.Use(requireAuth(sessionManager))

	// Todo一覧
	protected.GET("", func(c echo.Context) error {
		ctx := context.Background()
		todos, err := models.Todos.Query().All(ctx, db)
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoIndex(todos, csrfToken))
	})

	// Todo作成
	protected.POST("", func(c echo.Context) error {
		ctx := context.Background()
		title := c.FormValue("title")
		if title == "" {
			return c.Redirect(http.StatusFound, "/todos")
		}
		todo, err := models.Todos.Insert(&models.TodoSetter{
			Title: omit.From(title),
		}).One(ctx, db)
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoItem(todo, csrfToken))
	})

	// Todo完了状態の切り替え
	protected.POST("/:id/toggle", func(c echo.Context) error {
		ctx := context.Background()
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			return err
		}

		todo, err := models.Todos.Query(
			models.SelectWhere.Todos.ID.EQ(id),
		).One(ctx, db)
		if err != nil {
			return err
		}

		err = todo.Update(ctx, db, &models.TodoSetter{
			Completed: omit.From(!todo.Completed),
			UpdatedAt: omit.From(time.Now()),
		})
		if err != nil {
			return err
		}
		csrfToken := c.Get("csrf").(string)
		return render(c, http.StatusOK, views.TodoItem(todo, csrfToken))
	})

	// Todo削除
	protected.POST("/:id/delete", func(c echo.Context) error {
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
		return c.NoContent(http.StatusOK)
	})

	e.Logger.Fatal(e.Start(":8080"))
}

// render はTemplコンポーネントをEchoのレスポンスとして返すヘルパー関数
func render(c echo.Context, statusCode int, t templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(statusCode)
	return t.Render(c.Request().Context(), c.Response())
}

// requireAuth は認証を必要とするミドルウェア
func requireAuth(sessionManager *scs.SessionManager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID := sessionManager.GetInt64(c.Request().Context(), "user_id")
			if userID == 0 {
				return c.Redirect(http.StatusFound, "/auth/login")
			}
			return next(c)
		}
	}
}

// issuesToMap はzogのZogIssueListをフィールドごとのエラーマップに変換する
func issuesToMap(issues z.ZogIssueList) map[string][]string {
	errs := make(map[string][]string)
	for _, issue := range issues {
		if len(issue.Path) > 0 {
			key := issue.Path[0]
			errs[key] = append(errs[key], issue.Message)
		}
	}
	return errs
}
