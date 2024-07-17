package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

const url = "http://knvsh.gov.spb.ru/gosuslugi/svedeniya2/"

func formatDuration(duration time.Duration) string {
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	seconds := duration / time.Second

	return fmt.Sprintf("%d days %d hours %d minutes %d seconds", days, hours, minutes, seconds)
}

func isLetter(c rune) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func containsLetters(s string) bool {
	for _, c := range s {
		if !isLetter(c) {
			return false
		}
	}
	return true
}

func checkWebPage(number string) bool {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(time.Now().Format(time.RFC3339), "Error fetching webpage:", err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(time.Now().Format(time.RFC3339), "Error reading response body:", err)
		return false
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Checking for tracking number " + number)

	found := false
	doc.Find("table tr td").Each(func(indexth int, tablecell *goquery.Selection) {
		if strings.EqualFold(strings.TrimSpace(tablecell.Text()), number) {
			found = true
		}
	})

	return found
}

func startChecking(update tgbotapi.Update, bot *tgbotapi.BotAPI, number string, quit chan bool) {
	startTime := time.Now()

	for {
		select {
		case <-quit:
			return
		default:
			if checkWebPage(number) {
				endTime := time.Now()
				elapsedTime := endTime.Sub(startTime)
				formattedElapsedTime := formatDuration(elapsedTime)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Your documents are ready! Elapsed Time: %s", formattedElapsedTime))
				bot.Send(msg)
				log.Println("Found. Elapsed Time:", formattedElapsedTime)
				return
			} else {
				log.Println("Tracking number not found")
			}
			time.Sleep(time.Second * 10)
		}
	}
}

func main() {
	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer file.Close()

	log.SetOutput(file)

	os.Stdout = file
	os.Stderr = file

	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	authToken := os.Getenv("AUTH_TOKEN")
	if authToken == "" {
		log.Fatal("AUTH_TOKEN not set")
	}

	bot, err := tgbotapi.NewBotAPI(authToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	quit := make(chan bool)

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Send me tracking number")
				bot.Send(msg)
				continue
			case "stop":
				quit <- true
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Polling stopped")
				bot.Send(msg)
				continue
			case "help":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "This bot fetches the content of "+url+" every minute")
				bot.Send(msg)
				continue
			default:
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
				bot.Send(msg)
				continue
			}
		}

		number := update.Message.Text
		if !containsLetters(number) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Doesn't look like a valid tracking number, try again")
			bot.Send(msg)
		} else {
			go startChecking(update, bot, number, quit)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Started checking the webpage, wait for notification")
			bot.Send(msg)
		}
	}
}
