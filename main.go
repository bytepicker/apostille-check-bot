package main

import (
	"database/sql"
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
	_ "github.com/lib/pq"
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
		if isLetter(c) {
			return true
		}
	}
	return false
}

func checkWebPage(number string) bool {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error fetching webpage: %v", err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return false
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		log.Printf("Error parsing webpage: %v", err)
		return false
	}

	log.Printf("Checking for tracking number %s", number)

	found := false
	doc.Find("table tr td").Each(func(indexth int, tablecell *goquery.Selection) {
		if strings.EqualFold(strings.TrimSpace(tablecell.Text()), number) {
			found = true
		}
	})

	return found
}

func startChecking(update tgbotapi.Update, bot *tgbotapi.BotAPI, number string, quit chan bool, db *sql.DB) {
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
				log.Printf("Found tracking number %s. Elapsed Time: %s", number, formattedElapsedTime)

				if err := deleteTrackingNumber(db, number); err != nil {
					log.Printf("Error deleting tracking number %s: %v", number, err)
				}
				return
			} else {
				log.Printf("Tracking number %s not found", number)
			}
			time.Sleep(time.Minute)
		}
	}
}

func createTrackingNumber(db *sql.DB, userID int64, number string) error {
	sqlStatement := `
        INSERT INTO users (userid, tracking)
        VALUES ($1, $2)`
	_, err := db.Exec(sqlStatement, userID, number)
	return err
}

func deleteTrackingNumber(db *sql.DB, number string) error {
	sqlStatement := `
        DELETE FROM users
        WHERE tracking = $1`
	_, err := db.Exec(sqlStatement, number)
	return err
}

func loadTrackingNumbers(db *sql.DB) (map[int64][]string, error) {
	trackingNumbers := make(map[int64][]string)
	rows, err := db.Query("SELECT userid, tracking FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var userID int64
		var number string
		if err := rows.Scan(&userID, &number); err != nil {
			return nil, err
		}
		trackingNumbers[userID] = append(trackingNumbers[userID], number)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return trackingNumbers, nil
}

func initDB() (*sql.DB, error) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	pgUsername := os.Getenv("PG_USERNAME")
	pgPassword := os.Getenv("PG_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	if pgUsername == "" || pgPassword == "" || dbName == "" {
		log.Fatal("Database configuration not set")
	}

	psqlInfo := fmt.Sprintf("host=localhost port=5432 user=%s password=%s dbname=%s sslmode=disable", pgUsername, pgPassword, dbName)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}

func main() {
	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer file.Close()

	log.SetOutput(file)

	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

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

	trackingNumbers, err := loadTrackingNumbers(db)
	if err != nil {
		log.Fatalf("Failed to load tracking numbers: %v", err)
	}

	for userID, numbers := range trackingNumbers {
		for _, number := range numbers {
			go startChecking(tgbotapi.Update{Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: userID}}}, bot, number, quit, db)
		}
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if update.Message.IsCommand() {
			handleCommand(update, bot, quit)
			continue
		}

		number := update.Message.Text
		if containsLetters(number) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Doesn't look like a valid tracking number, try again")
			bot.Send(msg)
		} else {
			err := createTrackingNumber(db, update.Message.From.ID, number)
			if err != nil {
				log.Printf("Error inserting tracking number: %v", err)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Failed to save your tracking number, try again later")
				bot.Send(msg)
			} else {
				go startChecking(update, bot, number, quit, db)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Started checking the webpage, wait for notification")
				bot.Send(msg)
			}
		}
	}
}

func handleCommand(update tgbotapi.Update, bot *tgbotapi.BotAPI, quit chan bool) {
	switch update.Message.Command() {
	case "start":
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Send me tracking number")
		bot.Send(msg)
	case "stop":
		quit <- true
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Polling stopped")
		bot.Send(msg)
	case "help":
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "This bot fetches the content of "+url+" every minute")
		bot.Send(msg)
	default:
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I don't know that command")
		bot.Send(msg)
	}
}
