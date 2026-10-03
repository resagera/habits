package main

import (
	"os"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"runtime"

	_ "github.com/lib/pq"

	"habits/bot"
	"habits/internal/logger"
	"habits/internal/migration"
	"habits/internal/repository"
	"habits/internal/version"
)

func main() {
	print("App version: " + version.Get().SemVer() + " " + runtime.Version() + " " + runtime.GOOS + "_" + runtime.GOARCH + "\n")
	showVer := flag.Bool("v", false, "show version")
	//debugMode := flag.Bool("debug", false, "debug mode")
	flag.Parse()
	if *showVer {
	}

	lg, err := logger.New(logger.Config{
		Level:   slog.LevelInfo,
		File:    "tg-webapp.log", // лог в файл
		Console: false,           // + вывод в консоль
		JSON:    true,            // текстовый формат
	})
	if err != nil {
		slog.Info(err.Error())
		panic(err)
	}

	//db, err = sql.Open("sqlite3", "./data.db")
	db, err := sql.Open("postgres", "host=localhost port=5432 user=webapp_bot password=webapp_bot_pgpwd4habr dbname=webapp_bot sslmode=disable")
	if err != nil {
		slog.Info(err.Error())
		log.Fatal(err)
	}
	if err := migration.ApplyMigrations(db); err != nil {
		fmt.Errorf("failed to ApplyMigrations: %v", err)
	}
	repo := repository.NewRepo(db)
	bot, err := tgBot.NewTgBot(os.Getenv("BOT_TOKEN"))
	//initDB(db)
	router := NewRouter(bot, lg, repo)

	slog.Info("Server " + version.Get().SemVer() + " started on http://localhost:8080")
	logger.Fatal("Start server", http.ListenAndServe(":8080", router))
	select {}
}
