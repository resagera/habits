package tgBot

import (
	"errors"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Service struct {
	botAPi *tgbotapi.BotAPI
}

//func handleTelegramUpload(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
//	if update.Message == nil || update.Message.Document == nil {
//		return
//	}
//
//	doc := update.Message.Document
//	fileCfg := tgbotapi.FileConfig{FileID: doc.FileID}
//	file, err := bot.GetFile(fileCfg)
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка получения файла"))
//		return
//	}
//
//	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bot.Token, file.FilePath)
//	resp, err := http.Get(url)
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка загрузки"))
//		return
//	}
//	defer resp.Body.Close()
//
//	reader := csv.NewReader(resp.Body)
//	records, err := reader.ReadAll()
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Ошибка чтения CSV"))
//		return
//	}
//
//	user := fmt.Sprint(update.Message.Chat.ID)
//	for i, r := range records {
//		if i == 0 || len(r) < 3 {
//			continue
//		}
//		group, name, checked := r[0], r[1], r[2] == "true"
//		main.db.Exec("INSERT OR REPLACE INTO user_checks (user, group_name, name, checked) VALUES (?, ?, ?, ?)",
//			user, group, name, checked)
//	}
//
//	bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "✅ Чеклисты успешно загружены"))
//}

func NewTgBot(botToken string) (*Service, error) {
	if botToken == "" {
		botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if botToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, err
	}
	return &Service{
		botAPi: bot,
	}, nil
}

func (b *Service) SendDoc(userID int64, filename string, fileData []byte) error {
	file := tgbotapi.FileBytes{
		Name:  filename,
		Bytes: fileData,
	}

	msg := tgbotapi.NewDocument(userID, file)
	_, err := b.botAPi.Send(msg)
	return err
}

func (b *Service) SendText(userID int64, text string) error {
	msg := tgbotapi.NewMessage(userID, text)
	_, err := b.botAPi.Send(msg)
	return err
}
