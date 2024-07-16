package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
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

func checkWebPage(number string) bool {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println(time.Now().Format(time.RFC3339), "Error fetching webpage:", err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(time.Now().Format(time.RFC3339), "Error reading response body:", err)
		return false
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Checking for tracking number " + number)

	found := false
	doc.Find("table").Each(func(index int, tablehtml *goquery.Selection) {
		tablehtml.Find("tr").Each(func(indextr int, rowhtml *goquery.Selection) {
			rowhtml.Find("td").Each(func(indexth int, tablecell *goquery.Selection) {
				if strings.EqualFold(strings.TrimSpace(tablecell.Text()), number) {
					found = true
				}
			})
		})
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
				fmt.Println(time.Now().Format(time.RFC3339), "Ready")
				fmt.Println("Elapsed Time:", formattedElapsedTime)
				return
			} else {
				fmt.Println("Tracking number not found")
			}
			time.Sleep(time.Second * 10)
		}
	}
}

func main() {
	err := godotenv.Load()
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
	started := false

	for update := range updates {
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Send me tracking number")
				bot.Send(msg)
				started = true
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

		if started {
			number := update.Message.Text
			if _, err := strconv.Atoi(number); err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Doesn't look like a valid tracking number, try again")
				bot.Send(msg)
			} else {
				started = false
				go startChecking(update, bot, number, quit)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Started checking the webpage, wait for notification")
				bot.Send(msg)
			}
		}
	}
}
