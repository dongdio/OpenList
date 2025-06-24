package op

import (
	"time"

	"github.com/Xhofe/go-cache"

	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/pkg/errs"
	"github.com/dongdio/OpenList/pkg/singleflight"
	"github.com/dongdio/OpenList/pkg/utils"
)

// Cache for storing user information
var userCache = cache.NewMemCache(cache.WithShards[*model.User](2))

// Group for preventing duplicate user queries
var userG singleflight.Group[*model.User]

// Special user instances kept in memory for quick access
var guestUser *model.User
var adminUser *model.User

// GetAdmin returns the admin user, loading it from database if necessary
func GetAdmin() (*model.User, error) {
	if adminUser == nil {
		user, err := db.GetUserByRole(model.ADMIN)
		if err != nil {
			return nil, err
		}
		adminUser = user
	}
	return adminUser, nil
}

// GetGuest returns the guest user, loading it from database if necessary
func GetGuest() (*model.User, error) {
	if guestUser == nil {
		user, err := db.GetUserByRole(model.GUEST)
		if err != nil {
			return nil, err
		}
		guestUser = user
	}
	return guestUser, nil
}

// GetUserByRole retrieves a user with a specific role
func GetUserByRole(role int) (*model.User, error) {
	return db.GetUserByRole(role)
}

// GetUserByName retrieves a user by username, using cache when available
func GetUserByName(username string) (*model.User, error) {
	if username == "" {
		return nil, errs.EmptyUsername
	}

	// Check cache first
	if user, ok := userCache.Get(username); ok {
		return user, nil
	}

	// Use singleflight to prevent duplicate database queries
	user, err, _ := userG.Do(username, func() (*model.User, error) {
		user, err := db.GetUserByName(username)
		if err != nil {
			return nil, err
		}
		userCache.Set(username, user, cache.WithEx[*model.User](time.Hour))
		return user, nil
	})

	return user, err
}

// GetUserByID retrieves a user by ID
func GetUserByID(id uint) (*model.User, error) {
	return db.GetUserById(id)
}

// GetUsers retrieves a paginated list of users
func GetUsers(pageIndex, pageSize int) (users []model.User, count int64, err error) {
	return db.GetUsers(pageIndex, pageSize)
}

// CreateUser creates a new user
func CreateUser(user *model.User) error {
	user.BasePath = utils.FixAndCleanPath(user.BasePath)
	return db.CreateUser(user)
}

// DeleteUserByID deletes a user by ID
// Admin and guest users cannot be deleted
func DeleteUserByID(id uint) error {
	oldUser, err := db.GetUserById(id)
	if err != nil {
		return err
	}

	// Prevent deletion of special users
	if oldUser.IsAdmin() || oldUser.IsGuest() {
		return errs.DeleteAdminOrGuest
	}

	// Remove from cache
	userCache.Del(oldUser.Username)

	return db.DeleteUserById(id)
}

// UpdateUser updates a user's information
func UpdateUser(user *model.User) error {
	oldUser, err := db.GetUserById(user.ID)
	if err != nil {
		return err
	}

	// Clear cached instances of special users if they are being updated
	if user.IsAdmin() {
		adminUser = nil
	}
	if user.IsGuest() {
		guestUser = nil
	}

	// Remove from cache
	userCache.Del(oldUser.Username)

	// Clean path and update
	user.BasePath = utils.FixAndCleanPath(user.BasePath)
	return db.UpdateUser(user)
}

// Cancel2FAByUser disables two-factor authentication for a user
func Cancel2FAByUser(user *model.User) error {
	user.OtpSecret = ""
	return UpdateUser(user)
}

// Cancel2FAByID disables two-factor authentication for a user by ID
func Cancel2FAByID(id uint) error {
	user, err := db.GetUserById(id)
	if err != nil {
		return err
	}
	return Cancel2FAByUser(user)
}

// DelUserCache removes a user from cache
// Also clears special user instances if needed
func DelUserCache(username string) error {
	user, err := GetUserByName(username)
	if err != nil {
		return err
	}

	// Clear special user instances if needed
	if user.IsAdmin() {
		adminUser = nil
	}
	if user.IsGuest() {
		guestUser = nil
	}

	userCache.Del(username)
	return nil
}