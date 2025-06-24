package utils

import (
	"os"
	"strings"

	"github.com/bytedance/sonic"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

var Json = sonic.ConfigDefault

// WriteJsonToFile write struct to json file
func WriteJsonToFile(dst string, data any) bool {
	str, err := Json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Errorf("failed convert Conf to []byte:%s", err.Error())
		return false
	}
	err = os.WriteFile(dst, str, 0777)
	if err != nil {
		log.Errorf("failed to write json file:%s", err.Error())
		return false
	}
	return true
}

func GetBytes(b []byte, path ...string) gjson.Result {
	return gjson.GetBytes(b, strings.Join(path, "."))
}