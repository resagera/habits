package main

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	tgBot "habits/bot"
	"habits/internal/repository"
)

type RouteData struct {
	bot  *tgBot.Service
	log  *slog.Logger
	repo repository.Repository
}

func NewRouter(bot *tgBot.Service, lg *slog.Logger, repo repository.Repository) *mux.Router {
	rd := RouteData{
		bot:  bot,
		log:  lg,
		repo: repo,
	}
	router := mux.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rd.log.Info("request", slog.Any("url", r.Method+" "+r.RemoteAddr+r.URL.Path), slog.Any("header", r.Header))
			next.ServeHTTP(w, r)
		})
	})

	router.HandleFunc("/api/resagerhelper/get", rd.handleGet)
	router.HandleFunc("/api/resagerhelper/set_categories", rd.handleSetCategories)
	router.HandleFunc("/api/resagerhelper/toggle", rd.handleToggle)
	router.HandleFunc("/api/resagerhelper/marks", rd.handleGetMarks)
	router.HandleFunc("/api/resagerhelper/set_color", rd.handleSetColor)
	router.HandleFunc("/api/resagerhelper/set_name", rd.handleSetName)

	router.HandleFunc("/api/resagerhelper/get_checks", rd.getChecks).Methods("GET")
	router.HandleFunc("/api/resagerhelper/add_check_group", rd.addCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/toggle_check", rd.toggleCheck).Methods("POST")
	router.HandleFunc("/api/resagerhelper/rename_check_group", rd.renameCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/add_check_item", rd.addCheckItem).Methods("POST")
	router.HandleFunc("/api/resagerhelper/delete_check_group", rd.deleteCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/delete_check_item", rd.deleteCheckItem).Methods("POST")
	router.HandleFunc("/api/resagerhelper/export-checks", rd.handleExportChecks)

	router.HandleFunc("/api/resagerhelper/diary", rd.saveDiaryEntry).Methods("POST")
	router.HandleFunc("/api/resagerhelper/diary", rd.getDiaryEntries).Methods("GET")
	router.HandleFunc("/api/resagerhelper/diary/search", rd.searchDiaryEntries).Methods("GET")
	router.HandleFunc("/api/resagerhelper/diary/{id}", rd.updateDiaryEntry).Methods("PUT")
	router.HandleFunc("/api/resagerhelper/diary/{id}", rd.deleteDiaryEntry).Methods("DELETE")
	router.HandleFunc("/api/resagerhelper/diary/export", rd.exportDiaryHandler).Methods("GET")

	router.HandleFunc("/api/resagerhelper/settings/background", rd.saveBackgroundHandler).Methods("POST")
	router.HandleFunc("/api/resagerhelper/settings/background", rd.getBackgroundHandler).Methods("GET")
	router.HandleFunc("/api/resagerhelper/settings/background", rd.deleteBackgroundHandler).Methods("DELETE")
	router.HandleFunc("/api/resagerhelper/settings/theme", rd.saveThemeHandler).Methods("POST")
	router.HandleFunc("/api/resagerhelper/settings/theme", rd.getThemeHandler).Methods("GET")

	router.HandleFunc("/api/resagerhelper/currencies/list", rd.handleListUserCurrencies).Methods("GET")
	router.HandleFunc("/api/resagerhelper/currencies/add", rd.handleAddUserCurrency).Methods("POST")
	router.HandleFunc("/api/resagerhelper/currencies/remove", rd.handleRemoveUserCurrency).Methods("DELETE")
	router.HandleFunc("/api/resagerhelper/currencies/rates", rd.handleGetRates).Methods("GET")

	router.HandleFunc("/api/resagerhelper/metrics/create", rd.handleCreateMetric)
	router.HandleFunc("/api/resagerhelper/metrics/list", rd.handleListMetrics)
	router.HandleFunc("/api/resagerhelper/metrics", func(w http.ResponseWriter, r *http.Request) {
		// deletion uses DELETE on this path with id & user
		if r.Method == http.MethodDelete {
			rd.handleDeleteMetric(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	router.HandleFunc("/api/resagerhelper/metrics/value", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			rd.handleAddMetricValue(w, r)
		case http.MethodDelete:
			rd.handleDeleteMetricValue(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	router.HandleFunc("/api/resagerhelper/metrics/values", rd.handleGetMetricValues)
	router.HandleFunc("/api/resagerhelper/metrics/values_multi", rd.handleGetMetricValuesMulti)

	//router.Handle("/", http.FileServer(http.Dir("../frontend/webapp"))).Methods("GET")
	router.PathPrefix("/uploads/backgrounds/").Handler(
		http.StripPrefix("/uploads/backgrounds/",
			http.FileServer(http.Dir("./uploads/backgrounds"))))

	router.PathPrefix("/").Handler(http.FileServer(http.Dir("../frontend/webapp")))

	return router
}
