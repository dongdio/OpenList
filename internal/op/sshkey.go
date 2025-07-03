package op

import (
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
)

// CreateSSHPublicKey creates a new SSH public key for a user
// Returns an error and a boolean indicating if the error is due to validation (true) or system issues (false)
func CreateSSHPublicKey(key *model.SSHPublicKey) (error, bool) {
	// Check if a key with the same title already exists for this user
	_, err := db.GetSSHPublicKeyByUserTitle(key.UserId, key.Title)
	if err == nil {
		return errors.New("key with the same title already exists"), true
	}

	// Parse and validate the SSH public key
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.KeyStr))
	if err != nil {
		return err, false
	}

	// Set key metadata
	key.Fingerprint = ssh.FingerprintSHA256(pubKey)
	key.AddedTime = time.Now()
	key.LastUsedTime = key.AddedTime

	return db.CreateSSHPublicKey(key), true
}

// GetSSHPublicKeyByUserID retrieves a paginated list of SSH public keys for a user
func GetSSHPublicKeyByUserID(userID uint, pageIndex, pageSize int) (keys []model.SSHPublicKey, count int64, err error) {
	return db.GetSSHPublicKeyByUserId(userID, pageIndex, pageSize)
}

// GetSSHPublicKeyByIDAndUserID retrieves a specific SSH public key for a user
// Ensures the key belongs to the specified user
func GetSSHPublicKeyByIDAndUserID(id uint, userID uint) (*model.SSHPublicKey, error) {
	key, err := db.GetSSHPublicKeyById(id)
	if err != nil {
		return nil, err
	}

	// Verify the key belongs to the specified user
	if key.UserId != userID {
		return nil, errors.Errorf("SSH key %d does not belong to user %d", id, userID)
	}

	return key, nil
}

// UpdateSSHPublicKey updates an existing SSH public key
func UpdateSSHPublicKey(key *model.SSHPublicKey) error {
	return db.UpdateSSHPublicKey(key)
}

// DeleteSSHPublicKeyByID deletes an SSH public key by its ID
func DeleteSSHPublicKeyByID(keyID uint) error {
	return db.DeleteSSHPublicKeyById(keyID)
}