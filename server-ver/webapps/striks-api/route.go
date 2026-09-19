package main

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	tgBot "habits/bot"
)

type RouteData struct {
	bot *tgBot.Service
	log *slog.Logger
}

func NewRouter(bot *tgBot.Service, lg *slog.Logger) *mux.Router {
	rd := RouteData{
		bot: bot,
		log: lg,
	}
	router := mux.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rd.log.Info("request", slog.Any("url", r.Method+r.URL.Path+r.RemoteAddr), slog.Any("header", r.Header))
			next.ServeHTTP(w, r)
		})
	})

	router.HandleFunc("/api/resagerhelper/get", handleGet)
	router.HandleFunc("/api/resagerhelper/set_categories", handleSetCategories)
	router.HandleFunc("/api/resagerhelper/toggle", handleToggle)
	router.HandleFunc("/api/resagerhelper/marks", handleGetMarks)
	router.HandleFunc("/api/resagerhelper/set_color", handleSetColor)
	router.HandleFunc("/api/resagerhelper/set_name", handleSetName)

	router.HandleFunc("/api/resagerhelper/get_checks", getChecks).Methods("GET")
	router.HandleFunc("/api/resagerhelper/add_check_group", addCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/toggle_check", toggleCheck).Methods("POST")
	router.HandleFunc("/api/resagerhelper/rename_check_group", renameCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/add_check_item", addCheckItem).Methods("POST")
	router.HandleFunc("/api/resagerhelper/delete_check_group", deleteCheckGroup).Methods("POST")
	router.HandleFunc("/api/resagerhelper/delete_check_item", deleteCheckItem).Methods("POST")
	router.HandleFunc("/api/resagerhelper/export-checks", rd.handleExportChecks)

	router.HandleFunc("/api/resagerhelper/diary", saveDiaryEntry).Methods("POST")
	router.HandleFunc("/api/resagerhelper/diary", getDiaryEntries).Methods("GET")
	router.HandleFunc("/api/resagerhelper/diary/search", searchDiaryEntries).Methods("GET")
	router.HandleFunc("/api/resagerhelper/diary/{id}", updateDiaryEntry).Methods("PUT")
	router.HandleFunc("/api/resagerhelper/diary/{id}", deleteDiaryEntry).Methods("DELETE")
	router.HandleFunc("/api/resagerhelper/diary/export", rd.exportDiaryHandler).Methods("GET")

	router.HandleFunc("/api/resagerhelper/settings/background", saveBackgroundHandler).Methods("POST")
	router.HandleFunc("/api/resagerhelper/settings/background", getBackgroundHandler).Methods("GET")
	router.HandleFunc("/api/resagerhelper/settings/theme", saveThemeHandler).Methods("POST")
	router.HandleFunc("/api/resagerhelper/settings/theme", getThemeHandler).Methods("GET")

	router.HandleFunc("/api/resagerhelper/currencies/list", handleListUserCurrencies).Methods("GET")
	router.HandleFunc("/api/resagerhelper/currencies/add", handleAddUserCurrency).Methods("POST")
	router.HandleFunc("/api/resagerhelper/currencies/remove", handleRemoveUserCurrency).Methods("DELETE")
	router.HandleFunc("/api/resagerhelper/currencies/rates", handleGetRates).Methods("GET")

	router.HandleFunc("/api/resagerhelper/metrics/create", handleCreateMetric)
	router.HandleFunc("/api/resagerhelper/metrics/list", handleListMetrics)
	router.HandleFunc("/api/resagerhelper/metrics", func(w http.ResponseWriter, r *http.Request) {
		// deletion uses DELETE on this path with id & user
		if r.Method == http.MethodDelete {
			handleDeleteMetric(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	})
	router.HandleFunc("/api/resagerhelper/metrics/value", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleAddMetricValue(w, r)
		case http.MethodDelete:
			handleDeleteMetricValue(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	router.HandleFunc("/api/resagerhelper/metrics/values", handleGetMetricValues)
	router.HandleFunc("/api/resagerhelper/metrics/values_multi", handleGetMetricValuesMulti)

	//router.Handle("/", http.FileServer(http.Dir("../frontend/webapp"))).Methods("GET")
	router.PathPrefix("/uploads/backgrounds/").Handler(
		http.StripPrefix("/uploads/backgrounds/",
			http.FileServer(http.Dir("./uploads/backgrounds"))))

	router.PathPrefix("/").Handler(http.FileServer(http.Dir("../frontend/webapp")))

	return router
}
