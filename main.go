package main

import (
	"database/sql"
	"encoding/gob"
	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/chi-middleware/logrus-logger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ilyakaznacheev/cleanenv"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/sirupsen/logrus"
	"github.com/thechubbypanda/syncify/config"
	"github.com/thechubbypanda/syncify/model"
	"github.com/thechubbypanda/syncify/views"
	"golang.org/x/oauth2"
	"net/http"
	"path/filepath"
	"strings"
)

var cfg config.Config

var sm = scs.New()

func main() {
	err := cleanenv.ReadEnv(&cfg)
	if err != nil {
		logrus.Fatalln("error reading cfg: ", err)
		return
	}

	logrus.SetLevel(cfg.LogLevel)

	setAuthenticator(cfg)

	gob.Register(oauth2.Token{})

	dbFilename := filepath.Join("/data", "syncify.db")

	db, err := sql.Open("sqlite3", "file:"+dbFilename)
	if err != nil {
		logrus.Fatalln(err)
	}

	logrus.Traceln("opened database file: " + dbFilename)

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS sessions (token TEXT PRIMARY KEY, data BLOB NOT NULL, expiry REAL NOT NULL);")
	if err != nil {
		logrus.Fatalln(err)
	}

	logrus.Traceln("sessions db created")

	_, err = db.Exec("CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry);")
	if err != nil {
		logrus.Fatalln(err)
	}

	logrus.Traceln("created sessions expiry index")

	logrus.Infoln("initialized database")

	sm.Store = sqlite3store.New(db)

	sm.Cookie.Secure = strings.HasPrefix(cfg.Url, "https")
	sm.Cookie.HttpOnly = true
	sm.Cookie.Persist = false
	sm.Cookie.Name = "syncify_session"
	sm.Cookie.SameSite = http.SameSiteLaxMode

	r := chi.NewRouter()

	cspParts := []string{"default-src 'self'", "style-src 'self' 'unsafe-inline'"}
	scriptSources := []string{"unpkg.com"}
	connectSources := []string{"'self'"}
	if cfg.Plausible.ScriptUrl != "" {
		scriptSources = append(scriptSources, cfg.Plausible.Origin)
		connectSources = append(connectSources, cfg.Plausible.Origin)
	}
	cspParts = append(cspParts, "script-src "+strings.Join(scriptSources, " "))
	cspParts = append(cspParts, "connect-src "+strings.Join(connectSources, " "))

	r.Use(
		logger.Logger("router", logrus.StandardLogger()),
		middleware.StripSlashes,
		sm.LoadAndSave,
		middleware.SetHeader("Content-Security-Policy", strings.Join(cspParts, "; ")),
		middleware.SetHeader("Strict-Transport-Security", "max-age=2592000"),
		middleware.SetHeader("X-Frame-Options", "DENY"),
		middleware.SetHeader("X-Content-Type-Options", "nosniff"),
		middleware.SetHeader("Referrer-Policy", "strict-origin"),
		middleware.SetHeader("Permissions-Policy", "accelerometer=(), autoplay=(), camera=(), cross-origin-isolated=(), display-capture=(), encrypted-media=(), fullscreen=(), geolocation=(), gyroscope=(), keyboard-map=(), magnetometer=(), microphone=(), midi=(), payment=(), picture-in-picture=(), publickey-credentials-get=(), screen-wake-lock=(), sync-xhr=(), usb=(), xr-spatial-tracking=()"),
	)

	r.Route("/", func(r chi.Router) {
		r.Get("/", Root)
		r.Get("/sync", Sync)
		r.Get("/login", Login)
		r.Get("/logout", Logout)
		r.Get("/callback", Callback)
		r.Get("/*", http.StripPrefix("/", http.FileServer(http.Dir("static/"))).ServeHTTP)
	})

	logrus.Infoln("starting web server on", ":8000")
	httpErr := http.ListenAndServe(":8000", r)
	if httpErr != nil {
		logrus.Errorln(httpErr)
	}
}

func Root(w http.ResponseWriter, r *http.Request) {
	s, err := GetClient(r)
	if err != nil {
		logrus.Debugln(err)
		err := views.Root(model.Model{Plausible: &cfg.Plausible}).Render(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			logrus.Errorln(err)
		}
		return
	}
	user, err := s.CurrentUser(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		logrus.Errorln(err)
		return
	}

	err = views.Root(model.Model{
		Plausible: &cfg.Plausible,
		User:      user,
	}).Render(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		logrus.Errorln(err)
		return
	}
}
