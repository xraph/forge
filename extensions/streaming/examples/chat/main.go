package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

func main() {
	// Create container
	container := forge.NewContainer()

	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:          "Streaming Chat",
		Version:       "1.0.0",
		RouterOptions: []forge.RouterOption{forge.WithContainer(container)},
	})

	// Create router
	router := forge.NewRouter(forge.WithContainer(container))

	// Create streaming extension with local backend
	streamExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
		streaming.WithFeatures(true, true, true, true, true), // Enable all features
		streaming.WithConnectionLimits(10, 100, 100),
	).(*streaming.Extension)

	// Register extension
	if err := streamExt.Register(app); err != nil {
		log.Fatal("Failed to register streaming extension:", err)
	}

	// Start extension
	if err := streamExt.Start(context.Background()); err != nil {
		log.Fatal("Failed to start extension:", err)
	}

	// Register streaming routes
	if err := streamExt.RegisterRoutes(router, "/ws", "/sse"); err != nil {
		log.Fatal("Failed to register streaming routes:", err)
	}

	// REST API
	api := router.Group("/api/v1")

	// Create room
	api.POST("/rooms", func(ctx forge.Context) error {
		type CreateRoomRequest struct {
			Name        string `json:"name" validate:"required"`
			Description string `json:"description"`
		}

		var req CreateRoomRequest
		if err := ctx.Bind(&req); err != nil {
			return err
		}

		// Get manager from extension
		manager := streamExt.Manager()

		// Get user from auth (simulated)
		userID := ctx.Get("user_id")
		if userID == nil {
			userID = "anonymous"
		}

		// Create room (simplified - would need proper room implementation)
		roomID := uuid.New().String()

		// For now, just return success
		// In a real implementation, you would create a proper room instance
		_ = manager // Use manager to avoid unused variable error

		return ctx.JSON(200, map[string]any{
			"id":          roomID,
			"name":        req.Name,
			"description": req.Description,
		})
	})

	// List rooms
	api.GET("/rooms", func(ctx forge.Context) error {
		manager := streamExt.Manager()

		rooms, err := manager.ListRooms(ctx.Request().Context())
		if err != nil {
			return err
		}

		roomList := make([]map[string]any, 0, len(rooms))
		for _, room := range rooms {
			roomList = append(roomList, map[string]any{
				"id":          room.GetID(),
				"name":        room.GetName(),
				"description": room.GetDescription(),
				"owner":       room.GetOwner(),
			})
		}

		return ctx.JSON(200, roomList)
	})

	// Get room history
	api.GET("/rooms/:id/history", func(ctx forge.Context) error {
		manager := streamExt.Manager()
		roomID := ctx.Param("id")

		messages, err := manager.GetHistory(ctx.Request().Context(), roomID, streaming.HistoryQuery{
			Limit: 100,
		})
		if err != nil {
			return err
		}

		return ctx.JSON(200, messages)
	})

	// Get room members
	api.GET("/rooms/:id/members", func(ctx forge.Context) error {
		manager := streamExt.Manager()
		roomID := ctx.Param("id")

		members, err := manager.GetRoomMembers(ctx.Request().Context(), roomID)
		if err != nil {
			return err
		}

		memberList := make([]map[string]any, 0, len(members))
		for _, member := range members {
			memberList = append(memberList, map[string]any{
				"user_id":     member.GetUserID(),
				"role":        member.GetRole(),
				"joined_at":   member.GetJoinedAt(),
				"permissions": member.GetPermissions(),
			})
		}

		return ctx.JSON(200, memberList)
	})

	// Serve static files
	router.GET("/", func(ctx forge.Context) error {
		ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		ctx.Response().WriteHeader(200)
		_, err := ctx.Response().Write([]byte(indexHTML))
		return err
	})

	// Start server
	fmt.Println("Chat server running on http://localhost:8080")
	fmt.Println("Open http://localhost:8080 in your browser")

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

const indexHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Forge Streaming Chat</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        #messages { border: 1px solid #ccc; height: 400px; overflow-y: scroll; padding: 10px; margin-bottom: 10px; }
        .message { margin-bottom: 10px; }
        .message .user { font-weight: bold; color: #0066cc; }
        .message .time { color: #999; font-size: 0.8em; }
        #input { width: 70%; padding: 10px; }
        #send { width: 25%; padding: 10px; }
        #status { margin-top: 10px; color: #666; }
        .typing { color: #999; font-style: italic; }
        .room-list { margin-bottom: 20px; }
        .room { padding: 10px; border: 1px solid #ddd; margin-bottom: 5px; cursor: pointer; }
        .room.active { background-color: #e6f2ff; }
    </style>
</head>
<body>
    <h1>Forge Streaming Chat</h1>
    
    <div class="room-list">
        <h2>Rooms</h2>
        <div id="rooms"></div>
        <button onclick="createRoom()">Create Room</button>
    </div>

    <div id="current-room"></div>
    <div id="messages"></div>
    <div id="typing"></div>
    
    <input type="text" id="input" placeholder="Type a message..." />
    <button id="send" onclick="sendMessage()">Send</button>
    
    <div id="status">Connecting...</div>

    <script>
        let ws = null;
        let currentRoom = null;
        let username = 'User' + Math.floor(Math.random() * 1000);
        let typingTimeout = null;

        function connect() {
            ws = new WebSocket('ws://localhost:8080/ws');

            ws.onopen = () => {
                document.getElementById('status').textContent = 'Connected as ' + username;
                loadRooms();
            };

            ws.onmessage = (event) => {
                const msg = JSON.parse(event.data);
                handleMessage(msg);
            };

            ws.onclose = () => {
                document.getElementById('status').textContent = 'Disconnected';
                setTimeout(connect, 3000);
            };

            ws.onerror = (error) => {
                console.error('WebSocket error:', error);
            };
        }

        function handleMessage(msg) {
            if (msg.type === 'message' && msg.room_id === currentRoom) {
                addMessage(msg);
            } else if (msg.type === 'typing' && msg.room_id === currentRoom) {
                showTyping(msg);
            } else if (msg.type === 'presence') {
                updatePresence(msg);
            }
        }

        function addMessage(msg) {
            const div = document.createElement('div');
            div.className = 'message';
            const time = new Date(msg.timestamp).toLocaleTimeString();
            div.innerHTML = '<span class="user">' + msg.user_id + '</span> ' +
                           '<span class="time">' + time + '</span><br>' +
                           msg.data;
            document.getElementById('messages').appendChild(div);
            document.getElementById('messages').scrollTop = 
                document.getElementById('messages').scrollHeight;
        }

        function sendMessage() {
            const input = document.getElementById('input');
            if (!input.value || !currentRoom) return;

            const msg = {
                type: 'message',
                room_id: currentRoom,
                user_id: username,
                data: input.value,
                timestamp: new Date().toISOString()
            };

            ws.send(JSON.stringify(msg));
            input.value = '';
            stopTyping();
        }

        function startTyping() {
            if (!currentRoom) return;

            const msg = {
                type: 'typing',
                room_id: currentRoom,
                user_id: username,
                data: true
            };
            ws.send(JSON.stringify(msg));

            clearTimeout(typingTimeout);
            typingTimeout = setTimeout(stopTyping, 3000);
        }

        function stopTyping() {
            if (!currentRoom) return;

            const msg = {
                type: 'typing',
                room_id: currentRoom,
                user_id: username,
                data: false
            };
            ws.send(JSON.stringify(msg));
        }

        function showTyping(msg) {
            const typingDiv = document.getElementById('typing');
            if (msg.data) {
                typingDiv.innerHTML = '<span class="typing">' + msg.user_id + ' is typing...</span>';
            } else {
                typingDiv.innerHTML = '';
            }
        }

        async function loadRooms() {
            const response = await fetch('/api/v1/rooms');
            const rooms = await response.json();
            
            const roomsDiv = document.getElementById('rooms');
            roomsDiv.innerHTML = '';
            
            rooms.forEach(room => {
                const div = document.createElement('div');
                div.className = 'room';
                div.textContent = room.name;
                div.onclick = () => joinRoom(room.id, room.name);
                roomsDiv.appendChild(div);
            });
        }

        function joinRoom(roomId, roomName) {
            currentRoom = roomId;
            document.getElementById('current-room').innerHTML = '<h3>Room: ' + roomName + '</h3>';
            document.getElementById('messages').innerHTML = '';

            const msg = {
                type: 'join',
                room_id: roomId,
                user_id: username
            };
            ws.send(JSON.stringify(msg));

            // Load history
            loadHistory(roomId);
        }

        async function loadHistory(roomId) {
            const response = await fetch('/api/v1/rooms/' + roomId + '/history');
            const messages = await response.json();
            
            messages.reverse().forEach(msg => {
                addMessage(msg);
            });
        }

        async function createRoom() {
            const name = prompt('Room name:');
            if (!name) return;

            const response = await fetch('/api/v1/rooms', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: name, description: '' })
            });

            if (response.ok) {
                loadRooms();
            }
        }

        function updatePresence(msg) {
            console.log('Presence:', msg);
        }

        document.getElementById('input').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                sendMessage();
            } else {
                startTyping();
            }
        });

        connect();
    </script>
</body>
</html>`
