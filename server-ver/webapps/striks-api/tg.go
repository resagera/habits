package main

import (
	"encoding/csv"
	"fmt"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

func handleTelegramUpload(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message == nil || update.Message.Document == nil {
		return
	}

	doc := update.Message.Document
	fileCfg := tgbotapi.FileConfig{FileID: doc.FileID}
	file, err := bot.GetFile(fileCfg)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка получения файла"))
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bot.Token, file.FilePath)
	resp, err := http.Get(url)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка загрузки"))
		return
	}
	defer resp.Body.Close()

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка чтения CSV"))
		return
	}

	user := fmt.Sprint(update.Message.Chat.ID)
	for i, r := range records {
		if i == 0 || len(r) < 3 {
			continue
		}
		group, name, checked := r[0], r[1], r[2] == "true"
		db.Exec("INSERT OR REPLACE INTO user_checks (user, group_name, name, checked) VALUES (?, ?, ?, ?)",
			user, group, name, checked)
	}

	bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "✅ Чеклисты успешно загружены"))
}
