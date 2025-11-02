# Security Extension Example

This example demonstrates how to use the security extension for session and cookie management in Forge.

## Features

- Session management with in-memory store
- Secure cookie handling with HttpOnly, Secure, and SameSite attributes
- Session middleware for automatic session loading
- Protected routes requiring valid sessions
- Login/logout functionality
- Session data persistence and updates
- Cookie CRUD operations

## Running the Example

```bash
cd examples/security-example
go run main.go
```

The server will start on `http://localhost:8080`.

## API Endpoints

### Public Endpoints

#### GET /public
Public endpoint that doesn't require authentication.

```bash
curl http://localhost:8080/public
```

#### GET /health
Health check endpoint.

```bash
curl http://localhost:8080/health
```

### Authentication

#### POST /login
Create a session by logging in.

```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "password"}' \
  -c cookies.txt
```

This will:
- Validate credentials
- Create a new session
- Set a session cookie
- Return session details

### Protected Endpoints (Require Session)

#### GET /api/profile
Get the current user's profile from the session.

```bash
curl http://localhost:8080/api/profile \
  -b cookies.txt
```

#### POST /api/profile
Update the user's profile in the session.

```bash
curl -X POST http://localhost:8080/api/profile \
  -H "Content-Type: application/json" \
  -d '{"bio": "Software Engineer", "location": "San Francisco"}' \
  -b cookies.txt
```

#### POST /api/logout
Logout and destroy the session.

```bash
curl -X POST http://localhost:8080/api/logout \
  -b cookies.txt
```

### Cookie Management

#### GET /cookies/set
Set example cookies (theme and language).

```bash
curl http://localhost:8080/cookies/set \
  -c cookies.txt
```

#### GET /cookies/get
Retrieve all cookies.

```bash
curl http://localhost:8080/cookies/get \
  -b cookies.txt
```

#### GET /cookies/delete
Delete example cookies.

```bash
curl http://localhost:8080/cookies/delete \
  -b cookies.txt
```

## Testing Flow

1. **Login and create a session:**
```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "password"}' \
  -c cookies.txt -v
```

2. **Access protected profile endpoint:**
```bash
curl http://localhost:8080/api/profile \
  -b cookies.txt
```

3. **Update profile data:**
```bash
curl -X POST http://localhost:8080/api/profile \
  -H "Content-Type: application/json" \
  -d '{"bio": "DevOps Engineer", "location": "New York"}' \
  -b cookies.txt
```

4. **Verify the update:**
```bash
curl http://localhost:8080/api/profile \
  -b cookies.txt
```

5. **Logout:**
```bash
curl -X POST http://localhost:8080/api/logout \
  -b cookies.txt
```

6. **Try to access protected endpoint after logout (should fail):**
```bash
curl http://localhost:8080/api/profile \
  -b cookies.txt
```

## Configuration

The security extension is configured with:

- **Session Store:** In-memory (for production, use Redis)
- **Session Cookie Name:** `my_session`
- **Session TTL:** 24 hours
- **Auto-Renew:** Enabled (extends session on each access)
- **Cookie Secure:** False (set to true in production with HTTPS)
- **Cookie HttpOnly:** True (prevents XSS attacks)
- **Cookie SameSite:** Lax (CSRF protection)

## Production Considerations

For production deployments:

1. **Use Redis for session storage:**
   ```go
   security.WithSessionStore("redis"),
   security.WithRedisAddress("redis://localhost:6379"),
   ```

2. **Enable secure cookies:**
   ```go
   security.WithCookieSecure(true), // Requires HTTPS
   ```

3. **Consider session security:**
   - Enable IP address tracking: `WithSessionTrackIPAddress(true)`
   - Enable user agent tracking: `WithSessionTrackUserAgent(true)`
   - Set appropriate idle timeout
   - Implement CSRF protection

4. **Use environment variables for secrets:**
   ```go
   security.WithRedisPassword(os.Getenv("REDIS_PASSWORD")),
   ```

## Security Features

### Session Security
- Cryptographically secure session ID generation (256-bit random)
- Automatic session expiration
- Session renewal on access (idle timeout)
- Session tracking with IP and user agent (optional)
- Multi-session management per user

### Cookie Security
- **HttpOnly:** Prevents JavaScript access (XSS protection)
- **Secure:** Only sent over HTTPS (MitM protection)
- **SameSite:** CSRF protection (Lax, Strict, or None)
- **Domain and Path:** Scope control
- **MaxAge/Expires:** Explicit expiration

## Extension Architecture

The security extension follows Forge's extension pattern:

1. **Extension Registration:** Registers services with DI container
2. **Extension Start:** Connects to session store
3. **Extension Stop:** Gracefully disconnects
4. **Health Checks:** Verifies session store connectivity

## Middleware

The session middleware:
- Automatically loads sessions from cookies
- Stores sessions in request context
- Auto-renews sessions (optional)
- Handles session expiration
- Provides metrics and logging
- Supports path exclusions

## Error Handling

The extension provides specific errors:
- `ErrSessionNotFound`: Session doesn't exist
- `ErrSessionExpired`: Session has expired
- `ErrInvalidSession`: Session data is corrupted
- `ErrCookieNotFound`: Cookie doesn't exist
- `ErrInvalidCookie`: Cookie data is invalid

## Metrics

The extension tracks:
- `security.sessions.created`: Number of sessions created
- `security.sessions.retrieved`: Number of sessions retrieved
- `security.sessions.updated`: Number of sessions updated
- `security.sessions.deleted`: Number of sessions deleted
- `security.sessions.touched`: Number of sessions renewed
- `security.sessions.expired`: Number of expired sessions
- `security.sessions.active`: Current number of active sessions
- `security.sessions.cleaned_up`: Number of sessions cleaned up

## Further Reading

- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [MDN: Using HTTP cookies](https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies)
- [OWASP Secure Cookie Attribute](https://owasp.org/www-community/controls/SecureCookieAttribute)

