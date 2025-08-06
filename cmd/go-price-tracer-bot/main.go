package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gocolly/colly/v2"
)

type Product struct {
	URL         string
	TargetPrice float64
	LastPrice   float64
	ChatID      int64
}

var (
	products      = make(map[string]Product)
	productsMutex = &sync.Mutex{}
)

func main() {
	bot, err := tgbotapi.NewBotAPI("ВАШ_ТЕЛЕГРАМ_ТОКЕН")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Авторизован как %s", bot.Self.UserName)

	go checkPrices(bot)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		//msgText := update.Message.Text

		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(chatID, "Привет! Отправь мне ссылку на товар и желаемую цену в формате: /add <ссылка> <цена>")
				bot.Send(msg)
			case "add":
				handleAddCommand(bot, chatID, update.Message.CommandArguments())
			case "list":
				handleListCommand(bot, chatID)
			default:
				msg := tgbotapi.NewMessage(chatID, "Неизвестная команда.")
				bot.Send(msg)
			}
		} else {
			msg := tgbotapi.NewMessage(chatID, "Используйте команды для взаимодействия со мной. /start для помощи.")
			bot.Send(msg)
		}
	}
}

// Обработка команды /add
func handleAddCommand(bot *tgbotapi.BotAPI, chatID int64, args string) {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		msg := tgbotapi.NewMessage(chatID, "Неверный формат. Используйте: /add <ссылка> <цена>")
		bot.Send(msg)
		return
	}

	url := parts[0]
	priceStr := parts[1]

	targetPrice, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Цена должна быть числом.")
		bot.Send(msg)
		return
	}

	productsMutex.Lock()
	products[url] = Product{
		URL:         url,
		TargetPrice: targetPrice,
		LastPrice:   0, // Будет обновлено при первой проверке
		ChatID:      chatID,
	}
	productsMutex.Unlock()

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Товар по ссылке %s добавлен для отслеживания с целевой ценой %.2f", url, targetPrice))
	bot.Send(msg)
}

// Обработка команды /list
func handleListCommand(bot *tgbotapi.BotAPI, chatID int64) {
	productsMutex.Lock()
	defer productsMutex.Unlock()

	if len(products) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Список отслеживаемых товаров пуст.")
		bot.Send(msg)
		return
	}

	var response strings.Builder
	response.WriteString("Отслеживаемые товары:\n")
	for _, p := range products {
		if p.ChatID == chatID {
			response.WriteString(fmt.Sprintf("- URL: %s\n  Целевая цена: %.2f\n  Последняя цена: %.2f\n", p.URL, p.TargetPrice, p.LastPrice))
		}
	}

	msg := tgbotapi.NewMessage(chatID, response.String())
	bot.Send(msg)
}

func checkPrices(bot *tgbotapi.BotAPI) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		<-ticker.C
		log.Println("Начинаю проверку цен...")

		productsMutex.Lock()
		for url, product := range products {
			go scrapePrice(bot, url, product)
		}
		productsMutex.Unlock()
	}
}

func scrapePrice(bot *tgbotapi.BotAPI, url string, product Product) {
	c := colly.NewCollector()
	var currentPrice float64

	c.OnHTML(".price-value", func(e *colly.HTMLElement) {
		priceText := strings.ReplaceAll(e.Text, " ", "")
		priceText = strings.ReplaceAll(priceText, "₽", "")
		price, err := strconv.ParseFloat(priceText, 64)
		if err == nil {
			currentPrice = price
		}
	})

	c.Visit(url)

	if currentPrice > 0 {
		log.Printf("Текущая цена для %s: %.2f", url, currentPrice)

		productsMutex.Lock()
		// Обновляем последнюю цену
		updatedProduct := products[url]
		updatedProduct.LastPrice = currentPrice
		products[url] = updatedProduct

		productsMutex.Unlock()

		if currentPrice <= product.TargetPrice && product.LastPrice > currentPrice {
			message := fmt.Sprintf("Цена на товар снизилась!\n\n%s\n\nНовая цена: %.2f (Ваша цель: %.2f)", product.URL, currentPrice, product.TargetPrice)
			msg := tgbotapi.NewMessage(product.ChatID, message)
			bot.Send(msg)
		}
	} else {
		log.Printf("Не удалось получить цену для %s", url)
	}
}
