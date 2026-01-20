package fc_factory

import (
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/functions"
	_ "backend-server/internal/domain/tools/functions"
	"backend-server/internal/domain/tools/types"
	"encoding/json"
	"log"
)

func GetFCTools(plugins map[string]interface{}, conn types.Connection) *tools.FCTools {
	ft := tools.NewFCTools(conn)
	ft.RegisterFunction(functions.HandleExitIntentFunctionName, functions.NewHandleExitIntentFunction(nil))
	ft.RegisterFunction(functions.StartMeetingMinutesFunctionName, functions.NewStartMeetingMinutesFunction())
	ft.RegisterFunction(functions.StopMeetingMinutesFunctionName, functions.NewStopMeetingMinutesFunction())
	//ft.RegisterFunction(functions.TemporarySwitchVoiceFunctionName, functions.NewTemporarySwitchVoiceFunction(nil))
	//ft.RegisterFunction(functions.SwitchVoiceFunctionName, functions.NewSwitchVoiceFunction(nil))
	//ft.RegisterFunction(functions.ListVoiceOptionsFunctionName, functions.NewListVoiceOptionsFunction(nil))
	ft.RegisterFunction(functions.GetIPLocationFunctionName, functions.NewGetIPLocationFunction(nil))
	ft.RegisterFunction(functions.GenerateImageFunctionName, functions.NewGenerateImageFunction())
	ft.RegisterFunction(functions.EditImageFunctionName, functions.NewEditImageFunction())
	ft.RegisterFunction(functions.GenerateVideoFunctionName, functions.NewGenerateVideoFunction())
	ft.RegisterFunction(functions.PlayMusicFunctionName, functions.NewPlayMusicFunction())

	for name, config := range plugins {
		var args map[string]interface{}

		log.Printf("plugin: %s, configType: %v \n", name, config)
		configStr, ok := config.(string)
		if ok {
			err := json.Unmarshal([]byte(configStr), &args)
			if err != nil {
				continue
			}
		} else {
			args, ok = config.(map[string]interface{})
			if !ok {
				continue
			}
		}

		switch name {
		case functions.GetNewsFromChinaNewsFunctionName:
			ft.RegisterFunction(functions.GetNewsFromChinaNewsFunctionName, functions.NewGetNewsFromChinaNewsFunction(args))
		case functions.GetNewsFromNewsNowFunctionName:
			ft.RegisterFunction(functions.GetNewsFromNewsNowFunctionName, functions.NewGetNewsFromNewsNowFunction(args))
		case functions.GetTimeFunctionName:
			ft.RegisterFunction(functions.GetTimeFunctionName, functions.NewGetTimeFunction(args))
		case functions.GetWeatherFunctionName:
			ft.RegisterFunction(functions.GetWeatherFunctionName, functions.NewGetWeatherFunction(args))
		}
	}

	return ft
}
