package session

import log "backend-server/internal/infrastructure/logger"

func (s *ChatSession) StopSpeaking(isSendTtsStop bool) {
	if s != nil && s.clientState != nil {
		log.Infof("StopSpeaking invoked: device=%s session=%s status=%s mode=%s send_tts_stop=%v",
			s.clientState.DeviceID, s.clientState.SessionID, s.clientState.GetStatus(), s.clientState.ListenMode, isSendTtsStop)
	}
	s.ClearChatTextQueue()
	s.llmManager.ClearLLMResponseQueue()
	s.ttsManager.ClearTTSQueue()

	s.clientState.CancelSessionCtx()
	s.clientState.CancelAfterAsrCtx()

	if isSendTtsStop {
		s.serverTransport.SendTtsStop()
	}

}

func (s *ChatSession) MqttClose() {
	s.serverTransport.SendMqttGoodbye()
}
