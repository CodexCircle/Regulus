package console

// This file holds the Italian table. It is excluded from the misspell linter
// (.golangci.yml): misspell checks English only and takes Italian words such as
// "integrato" or "Impossibile" for misspellings.

// it is the Italian text. Technical words Italian users also say in English
// (hub, dashboard, token) stay in English, as in the glossary. Keep the
// placeholders (%s) and the key letters of the help lines in step with en.
var it = strs{
	Title: "Regulus",

	TabStatus:   "Stato",
	TabSettings: "Impostazioni",
	TabLogs:     "Log",
	TabHealth:   "Controlli",
	TabActions:  "Azioni",

	Running: "in esecuzione",
	Stopped: "non in esecuzione",
	Service: "servizio",

	Hub:          "Hub",
	Version:      "Versione",
	Dashboard:    "Dashboard",
	AlsoOn:       "anche su",
	AgentPort:    "Porta degli agenti",
	BuiltinAgent: "Agente integrato",
	DataDir:      "Directory dei dati",
	Database:     "Database",
	ConfigFile:   "Configurazione",
	Uptime:       "attivo da",
	On:           "attivo",
	Off:          "spento",
	None:         "nessuno",
	Agents:       "Agenti connessi",
	NoAgents:     "Nessun agente connesso.",
	SetupTitle:   "Primo avvio",
	SetupText:    "Non esiste ancora un amministratore. Apri la dashboard e inserisci questo token di configurazione iniziale, valido una volta:",

	SettingHeader:  [3]string{"Impostazione", "Valore", "Origine"},
	Default:        "predefinito",
	AppliesNow:     "Si applica appena viene salvata.",
	AppliesRestart: "L'hub la legge all'avvio: riavvialo per applicarla.",
	EnvOverride:    "Una variabile d'ambiente prevale sul file: salvare qui non cambia ciò che usa l'hub.",
	Editing:        "Modifica",
	Valid:          "verrà salvato come",
	Choices:        "scelte",

	LogsEmpty:       "Nessuna riga di log.",
	LogsUnavailable: "Impossibile leggere il log",
	LogFilters:      [4]string{"tutti i livelli", "info e superiori", "avvisi ed errori", "solo errori"},
	Following:       "in diretta",

	Loading: "Caricamento…",

	HelpMain:    "tab/←/→ sezione  ↑/↓ muovi  invio modifica  r riavvia  p reimposta la password admin  q esci",
	HelpEdit:    "invio salva  esc annulla  tab prossima scelta",
	HelpConfirm: "y sì  n no",

	RestartAsk:  "Riavviare ora il servizio regulus?",
	ResetAsk:    "Creare un link monouso per scegliere una nuova password da amministratore?",
	RestartHint: "Riavvia l'hub per applicarla: premi r, oppure esegui: sudo systemctl restart regulus",

	ActionsIntro:        "Premi il tasto di un'azione, oppure selezionala con ↑/↓ e premi invio. Ognuna chiede conferma prima di agire.",
	ActRestart:          "Riavvia il servizio",
	ActRestartInfo:      "Riavvia regulus.service, per applicare le impostazioni lette all'avvio. Servono root o sudo senza password.",
	ActReset:            "Reimposta la password dell'amministratore",
	ActResetInfo:        "Crea un link monouso per scegliere una nuova password, quando nessuno riesce ad accedere. Servono root o l'utente dell'hub.",
	ActAgents:           "Attiva gli agent remoti",
	ActAgentsInfo:       "Imposta agent_listen a :8443 e riavvia l'hub, così altri server possono collegarsi. Serve root.",
	ActUnavailable:      "non disponibile",
	AgentsAsk:           "Attivare gli agent remoti sulla porta 8443 e riavviare l'hub?",
	NewAgentTitle:       "Aggiungere un server",
	NewAgentText:        "Gli agent remoti sono attivi. Sul nuovo server l'installer richiede:",
	NewAgentAddress:     "Indirizzo dell'hub",
	NewAgentFingerprint: "Impronta della CA",
	NewAgentToken:       "Token: crealo nella dashboard, Server → Aggiungi un server (mostra il comando completo).",

	MsgAgentsFailed:       "Impossibile attivare gli agent remoti: %s. Apri la console con sudo, oppure imposta agent_listen a mano.",
	MsgAgentsSaved:        "Salvato agent_listen = %s.",
	MsgAgentsOn:           "Gli agent remoti sono attivi, porta 8443. Aprila nel firewall verso i server che gestisci.",
	MsgRestartFailed:      "Impossibile riavviare: %s. Esegui: sudo systemctl restart %s",
	MsgRestartUnavailable: "Qui il riavvio non è disponibile. Esegui: sudo systemctl restart %s",
	MsgRestarting:         "Riavvio in corso…",
	MsgRestarted:          "Il servizio è stato riavviato.",
	MsgLinkFailed:         "Impossibile creare il link: %s",
	MsgLinkReady:          "Apri il link qui sotto per scegliere una nuova password. Funziona una sola volta.",
	MsgMakingLink:         "Creo il link…",
	MsgTurningOn:          "Attivo gli agent remoti…",
	MsgSaved:              "Salvato %s = %s.",
	ResetLinkTitle:        "Link per reimpostare la password",
}
