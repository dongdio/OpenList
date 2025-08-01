/*
Package cmd
Copyright © 2022 Noah Hsu<i@nn.ci>
*/
package cmd

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/dongdio/OpenList/v4/consts"
	_ "github.com/dongdio/OpenList/v4/drivers"
	"github.com/dongdio/OpenList/v4/initialize"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type KV[V any] map[string]V

type Drivers KV[KV[any]]

var frontendPath string

func firstUpper(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func convert(s string) string {
	ss := strings.Split(s, "_")
	ans := strings.Join(ss, " ")
	return firstUpper(ans)
}

func writeFile(name string, data any) {
	f, err := os.Open(fmt.Sprintf("%s/src/lang/en/%s.json", frontendPath, name))
	if err != nil {
		log.Errorf("failed to open %s.json: %+v", name, err)
		return
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		log.Errorf("failed to read %s.json: %+v", name, err)
		return
	}
	oldData := make(map[string]any)
	newData := make(map[string]any)
	err = utils.JSONTool.Unmarshal(content, &oldData)
	if err != nil {
		log.Errorf("failed to unmarshal %s.json: %+v", name, err)
		return
	}
	content, err = utils.JSONTool.Marshal(data)
	if err != nil {
		log.Errorf("failed to marshal json: %+v", err)
		return
	}
	err = utils.JSONTool.Unmarshal(content, &newData)
	if err != nil {
		log.Errorf("failed to unmarshal json: %+v", err)
		return
	}
	if reflect.DeepEqual(oldData, newData) {
		log.Infof("%s.json no changed, skip", name)
	} else {
		log.Infof("%s.json changed, update file", name)
		utils.WriteJSONToFile(fmt.Sprintf("lang/%s.json", name), newData)
	}
}

func generateDriversJson() {
	drivers := make(Drivers)
	drivers["drivers"] = make(KV[any])
	drivers["config"] = make(KV[any])
	driverInfoMap := op.GetDriverInfoMap()
	for k, v := range driverInfoMap {
		drivers["drivers"][k] = convert(k)
		items := make(KV[any])
		config := map[string]string{}
		if v.Config.Alert != "" {
			alert := strings.SplitN(v.Config.Alert, "|", 2)
			if len(alert) > 1 {
				config["alert"] = alert[1]
			}
		}
		drivers["config"][k] = config
		for i := range v.Additional {
			item := v.Additional[i]
			items[item.Name] = convert(item.Name)
			if item.Help != "" {
				items[fmt.Sprintf("%s-tips", item.Name)] = item.Help
			}
			if item.Type == consts.TypeSelect && len(item.Options) > 0 {
				options := make(KV[string])
				_options := strings.Split(item.Options, ",")
				for _, o := range _options {
					options[o] = convert(o)
				}
				items[fmt.Sprintf("%ss", item.Name)] = options
			}
		}
		drivers[k] = items
	}
	writeFile("drivers", drivers)
}

func generateSettingsJson() {
	settings := initialize.InitialSettings()
	settingsLang := make(KV[any])
	for _, setting := range settings {
		settingsLang[setting.Key] = convert(setting.Key)
		if setting.Help != "" {
			settingsLang[fmt.Sprintf("%s-tips", setting.Key)] = setting.Help
		}
		if setting.Type == consts.TypeSelect && len(setting.Options) > 0 {
			options := make(KV[string])
			_options := strings.Split(setting.Options, ",")
			for _, o := range _options {
				options[o] = convert(o)
			}
			settingsLang[fmt.Sprintf("%ss", setting.Key)] = options
		}
	}
	writeFile("settings", settingsLang)
	// utils.WriteJsonToFile("lang/settings.json", settingsLang)
}

// LangCmd represents the lang command
var LangCmd = &cobra.Command{
	Use:   "lang",
	Short: "Generate language json file",
	Run: func(cmd *cobra.Command, args []string) {
		frontendPath, _ = cmd.Flags().GetString("frontend-path")
		initialize.InitConfig()
		err := os.MkdirAll("lang", 0777)
		if err != nil {
			utils.Log.Fatalf("failed create folder: %s", err.Error())
		}
		generateDriversJson()
		generateSettingsJson()
	},
}

func init() {
	RootCmd.AddCommand(LangCmd)

	// Add frontend-path flag
	LangCmd.Flags().String("frontend-path", "../OpenList-Frontend", "Path to the frontend project directory")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// langCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// langCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
