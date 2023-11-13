package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var bot *tgbotapi.BotAPI

const dataFilePath = "R:/golang/ebuchi/userdata.json"

const ModeHTML = "HTML"

// Структура для хранения данных о пользователе
type UserData struct {
	Balance        int       // Баланс пользователя
	LastGameTime   time.Time // Время последней игры
	LastGameTS     int64     // Unix timestamp последней игры
	GamesPlayed    int       // Количество сыгранных игр
	IsBlacklisted  bool      // Признак нахождения в черном списке
	SpamProtection int       // Защита от спама
}

var (
	userDataMu     sync.Mutex
	userData       map[int64]*UserData // Мапа для хранения данных о пользователях (по их айди)
	lastGame       map[int64]time.Time // Мапа для хранения времени последней игры пользователя
	spamProtection map[int64]time.Time
)

func connectWithTelegram() {
	var err error
	if bot, err = tgbotapi.NewBotAPI("6919106470:AAEYXfoBB64Jeod_qk5ceKdtmFudy1HPnA4"); err != nil {
		panic("Failed to connect to Telegram")
	}
}

func loadUserData() {
	fileData, err := ioutil.ReadFile(dataFilePath)
	if err != nil {
		fmt.Println("Error reading data file:", err)
		return
	}

	userDataMu.Lock()
	defer userDataMu.Unlock()

	err = json.Unmarshal(fileData, &userData)
	if err != nil {
		fmt.Println("Error unmarshalling data:", err)
		return
	}
}

func saveUserData() {
	userDataMu.Lock()
	defer userDataMu.Unlock()

	fileData, err := json.Marshal(userData)
	if err != nil {
		fmt.Println("Ошибка при маршалинге данных:", err)
		return
	}

	err = ioutil.WriteFile(dataFilePath, fileData, 0644)
	if err != nil {
		fmt.Println("Ошибка при записи файла данных:", err)
	}
}

func resetThrows() {
	for {
		currentTime := time.Now().UTC()
		if currentTime.Hour() == 0 && currentTime.Minute() == 0 && currentTime.Second() == 0 {
			userDataMu.Lock()
			for _, user := range userData {
				user.GamesPlayed = 0
			}
			userDataMu.Unlock()
		}
		time.Sleep(1 * time.Minute) // Проверка каждую минуту
	}
}

func sendMessage(chatID int64, msg string) {
	msgConfig := tgbotapi.NewMessage(chatID, msg)
	bot.Send(msgConfig)
}

func isMessageForIbuchi(update *tgbotapi.Update) bool {
	if update.Message == nil || update.Message.Dice == nil {
		return false
	}

	// Проверяем, что текст сообщения равен "🏀"
	return update.Message.Dice.Emoji == "🏀"
}

func evaluateUserThrow(update *tgbotapi.Update) {
	// Ожидание перед оглашением результатов
	time.Sleep(2 * time.Second)
	diceValue := update.Message.Dice.Value // логика для определения значения броска

	// Обновляем данные о пользователе
	userDataMu.Lock()
	defer userDataMu.Unlock()

	user, ok := userData[update.Message.From.ID]
	if !ok {
		user = &UserData{Balance: 50} // Новый пользователь получает 50 монет
		userData[update.Message.From.ID] = user
	}

	// Проверка на защиту от спама
	if protectionTime, exists := spamProtection[update.Message.From.ID]; exists && time.Since(protectionTime) < 24*time.Hour {
		// Пользователь пытается сыграть в течение 24 часов после достижения лимита
		sendMessage(update.Message.Chat.ID, "Чел, ты люто запотел🥵.\nПокидаешь через 24 часа.")
		return
	}

	switch diceValue {
	case 1:
		user.Balance -= 5
	case 2:
		user.Balance -= 4
	case 3:
		// Ничего не убавляем
	case 4:
		user.Balance += 4
	case 5:
		user.Balance += 5
	}

	sendMessage(update.Message.Chat.ID, fmt.Sprintf("Твой бросок на: %d из 5.\nБаланс: %d монеток\n‼️У тебя осталось %d броска(ов) перед перерывом.‼️", diceValue, user.Balance, 4-user.GamesPlayed))

	// Обновляем время последней игры и timestamp
	user.LastGameTime = time.Now()
	user.LastGameTS = time.Now().Unix()

	// Увеличиваем количество сыгранных игр
	user.GamesPlayed++

	// Если пользователь сыграл 5 раз, обнуляем счетчик и запускаем защиту на 10 минут
	if user.GamesPlayed == 5 {
		sendMessage(update.Message.Chat.ID, "Чел, ты люто запотел🥵.\nПокидаешь через 24 часа.")
		spamProtection[update.Message.From.ID] = time.Now()
	}
}

func showTopPlayers(chatID int64) {
	userDataMu.Lock()
	defer userDataMu.Unlock()

	// Сначала создаем слайс с элементами структуры
	var sortedUsers []struct {
		UserName string
		Balance  int
	}

	// Заполняем слайс данными из карты
	for id, data := range userData {
		// Получаем информацию о пользователе
		member, err := bot.GetChatMember(tgbotapi.GetChatMemberConfig{tgbotapi.ChatConfigWithUser{
			ChatID: chatID, UserID: id}})

		if err != nil {
			// Обработка ошибки, если не удается получить информацию о пользователе
			fmt.Println("Error getting user info:", err)
			continue
		}

		// Получаем UserName пользователя
		userName := member.User.UserName
		if userName == "" {
			// Если у пользователя нет UserName, используем его айди в формате @123456789
			userName = fmt.Sprintf("@%d", id)
		}

		sortedUsers = append(sortedUsers, struct {
			UserName string
			Balance  int
		}{UserName: userName, Balance: data.Balance})
	}

	// Проверяем, есть ли вообще пользователи
	if len(sortedUsers) == 0 {
		sendMessage(chatID, "Пока нет данных о пользователях.")
		return
	}

	// Сортировка по убыванию баланса
	sort.Slice(sortedUsers, func(i, j int) bool {
		return sortedUsers[i].Balance > sortedUsers[j].Balance
	})

	// Создаем сообщение с топ-10 игроками
	var topMessage strings.Builder
	topMessage.WriteString("Топ-10 чёрномазых:\n")

	// Проверяем, есть ли вообще 10 пользователей в списке)
	topCount := 10
	if len(sortedUsers) < 10 {
		topCount = len(sortedUsers)
	}

	for i := 0; i < topCount; i++ {
		// Добавляем информацию в сообщение
		topMessage.WriteString(fmt.Sprintf("%d. %s: %d монет\n", i+1, sortedUsers[i].UserName, sortedUsers[i].Balance))
	}

	sendMessage(chatID, topMessage.String())
}

func main() {
	connectWithTelegram()
	loadUserData()
	go resetThrows()
	updateConfig := tgbotapi.NewUpdate(0)
	updates := bot.GetUpdatesChan(updateConfig)

	userData = make(map[int64]*UserData)
	lastGame = make(map[int64]time.Time)
	spamProtection = make(map[int64]time.Time)

	for update := range updates {
		if update.Message != nil {
			chatID := update.Message.Chat.ID // обновляем chatID в соответствии с новым сообщением
			switch strings.ToLower(update.Message.Text) {
			case "/start":
				messageText := "Привет! Я - <b>Ебучий</b> 🐶.\nЯ настолько тёмный, что научу вас бросать этот чёртов мяч в кольцо!⛹🏾‍\nКидай мяч, а я оценю, что ты умеешь. 🏀"

				// Создаем сообщение с HTML-разметкой
				message := tgbotapi.NewMessage(chatID, messageText)
				message.ParseMode = tgbotapi.ModeHTML

				bot.Send(message)
			case "/help":
				messageText := "Ты реально <b>долбоёб?</b>🤨Я же объяснил как кидать мяч!\nКидай мне 🏀, чтобы сыграть."
				message := tgbotapi.NewMessage(chatID, messageText)
				message.ParseMode = tgbotapi.ModeHTML

				bot.Send(message)
			case "/ebutop":
				showTopPlayers(chatID)
			default:
				if isMessageForIbuchi(&update) {
					sendMessage(chatID, "Ты кинул мяч!\nСейчас посмотрим, что получится!😯")
					evaluateUserThrow(&update)
					saveUserData() // Сохранение данных после каждого броска
				}
			}
		}
	}
}
