package main

import (
	"fmt"
	"log"
	handler "monitor/server"

	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
)

type server struct {
	subscriberMessageBuffer int
	subscribersMu           sync.Mutex
	subscribers             map[*subscriber]struct{}
	app                     *fiber.App
}

type subscriber struct {
	msgs chan []byte
	conn *websocket.Conn
}

func NewServer() *server {
	// Initialize HTML template engine
	engine := html.New("./client", ".html")

	// Create Fiber app with template engine
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Add logger middleware
	app.Use(logger.New())

	// WebSocket upgrade middleware
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	s := &server{
		subscriberMessageBuffer: 10,
		subscribers:             make(map[*subscriber]struct{}),
		app:                     app,
	}

	// Static files
	app.Static("/", "./static")

	// Routes
	app.Get("/", s.indexHandler)

	// WebSocket handler
	app.Get("/ws", websocket.New(s.websocketHandler))

	return s
}

func (s *server) indexHandler(c *fiber.Ctx) error {
	// Render the main page template
	return c.Render("index", fiber.Map{
		"Title": "System Monitor",
	})
}

func (s *server) websocketHandler(c *websocket.Conn) {
	subscriber := &subscriber{
		msgs: make(chan []byte, s.subscriberMessageBuffer),
		conn: c,
	}

	s.addSubscriber(subscriber)
	defer s.removeSubscriber(subscriber)

	fmt.Println("WebSocket connection established")

	// Handle incoming messages and send outgoing messages
	for {
		select {
		case msg := <-subscriber.msgs:
			err := c.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				fmt.Printf("WebSocket write error: %v\n", err)
				return
			}
		default:
			// Check if connection is still alive
			if err := c.WriteMessage(websocket.PingMessage, nil); err != nil {
				fmt.Printf("WebSocket ping error: %v\n", err)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (s *server) addSubscriber(subscriber *subscriber) {
	s.subscribersMu.Lock()
	s.subscribers[subscriber] = struct{}{}
	s.subscribersMu.Unlock()
	fmt.Printf("Added subscriber, total: %d\n", len(s.subscribers))
}

func (s *server) removeSubscriber(subscriber *subscriber) {
	s.subscribersMu.Lock()
	delete(s.subscribers, subscriber)
	s.subscribersMu.Unlock()
	fmt.Printf("Removed subscriber, total: %d\n", len(s.subscribers))
	close(subscriber.msgs)
}

func (s *server) publishMsg(msg []byte) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	for subscriber := range s.subscribers {
		select {
		case subscriber.msgs <- msg:
		default:
			// Channel is full, remove subscriber
			fmt.Println("Subscriber channel full, removing subscriber")
			delete(s.subscribers, subscriber)
			close(subscriber.msgs)
		}
	}
}

func (s *server) startDataPublisher() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C{

				systemData, err := handler.GetSystemSection()
				if err != nil {
					fmt.Printf("Error getting system data: %v\n", err)
					continue
				}

				diskData, err := handler.GetDiskSection()
				if err != nil {
					fmt.Printf("Error getting disk data: %v\n", err)
					continue
				}

				cpuData, err := handler.GetCpuSection()
				if err != nil {
					fmt.Printf("Error getting CPU data: %v\n", err)
					continue
				}

				timeStamp := time.Now().Format("2006-01-02 15:04:05")

				// Create HTMX-compatible message
				msg := []byte(fmt.Sprintf(`
				<div hx-swap-oob="innerHTML:#update-timestamp">
					<p><i style="color: green" class="fa fa-circle"></i> %s</p>
				</div>
				<div hx-swap-oob="innerHTML:#system-data">%s</div>
				<div hx-swap-oob="innerHTML:#cpu-data">%s</div>
				<div hx-swap-oob="innerHTML:#disk-data">%s</div>`,
					timeStamp, systemData, cpuData, diskData))

				s.publishMsg(msg)
			}
	}()
}

func main() {
	fmt.Println("Starting Fiber monitor server on port 8080")

	s := NewServer()

	// Start the data publisher goroutine
	s.startDataPublisher()

	// Start the server
	log.Fatal(s.app.Listen(":8080"))
}
