package handlers

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// Register handles user registration
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Check if any users exist
		hasUsers, err := h.db.HasAnyUsers()
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if hasUsers {
			// Redirect to login if a user already exists
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Render register page
		h.renderTemplate(w, "register.html", nil)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if any users exist before allowing registration
	hasUsers, err := h.db.HasAnyUsers()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if hasUsers {
		http.Error(w, "Registration is closed. An account already exists.", http.StatusForbidden)
		return
	}

	// Parse form
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Create user
	err = h.db.CreateUser(username, string(hashedPassword))
	if err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Create session
	session := h.sessions.New()
	session.Set("username", username)
	session.Set("authenticated", true)
	h.sessions.Save(w, session)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Login handles user login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Check if any users exist
		hasUsers, err := h.db.HasAnyUsers()
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if !hasUsers {
			// Redirect to register if no users exist
			http.Redirect(w, r, "/register", http.StatusSeeOther)
			return
		}

		// Render login page
		h.renderTemplate(w, "login.html", nil)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Get user from database
	user, err := h.db.GetUserByUsername(username)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if user == nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create session
	session := h.sessions.New()
	session.Set("username", username)
	session.Set("authenticated", true)
	h.sessions.Save(w, session)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles user logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	session := h.sessions.Get(r)
	if session != nil {
		h.sessions.Destroy(w, session)
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// AuthMiddleware checks if user is authenticated
func (h *Handler) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session := h.sessions.Get(r)
		if session == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		authenticated, ok := session.Get("authenticated").(bool)
		if !ok || !authenticated {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}
