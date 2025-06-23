package op

import (
	"time"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/model"
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

// GetSSHPublicKeyByUserId retrieves a paginated list of SSH public keys for a user
func GetSSHPublicKeyByUserId(userId uint, pageIndex, pageSize int) (keys []model.SSHPublicKey, count int64, err error) {
	return db.GetSSHPublicKeyByUserId(userId, pageIndex, pageSize)
}

// GetSSHPublicKeyByIdAndUserId retrieves a specific SSH public key for a user
// Ensures the key belongs to the specified user
func GetSSHPublicKeyByIdAndUserId(id uint, userId uint) (*model.SSHPublicKey, error) {
	key, err := db.GetSSHPublicKeyById(id)
	if err != nil {
		return nil, err
	}

	// Verify the key belongs to the specified user
	if key.UserId != userId {
		return nil, errors.Errorf("SSH key %d does not belong to user %d", id, userId)
	}

	return key, nil
}

// UpdateSSHPublicKey updates an existing SSH public key
func UpdateSSHPublicKey(key *model.SSHPublicKey) error {
	return db.UpdateSSHPublicKey(key)
}

// DeleteSSHPublicKeyById deletes an SSH public key by its ID
func DeleteSSHPublicKeyById(keyId uint) error {
	return db.DeleteSSHPublicKeyById(keyId)
}
