package console

import "strings"

// Every word the console shows that is not data lives here, so that a
// translation replaces one value and touches no other file.
type strs struct {
	Title string

	TabStatus, TabSettings, TabLogs, TabHealth, TabActions string

	Running, Stopped string
	Service          string

	Hub, Version, Dashboard, AlsoOn, AgentPort, BuiltinAgent, DataDir, Database, ConfigFile, Uptime string
	On, Off, None                                                                                   string
	Agents, NoAgents                                                                                string
	SetupTitle, SetupText                                                                           string

	SettingHeader  [3]string
	Default        string
	AppliesNow     string
	AppliesRestart string
	EnvOverride    string
	Editing        string
	Valid          string
	Choices        string

	LogsEmpty, LogsUnavailable string
	LogFilters                 [4]string
	Following                  string

	Loading string

	HelpMain, HelpEdit, HelpConfirm string

	RestartAsk, ResetAsk string
	RestartHint          string

	ActionsIntro                         string
	ActRestart, ActRestartInfo           string
	ActReset, ActResetInfo               string
	ActAgents, ActAgentsInfo             string
	ActUnavailable                       string
	AgentsAsk                            string
	NewAgentTitle, NewAgentText          string
	NewAgentAddress, NewAgentFingerprint string
	NewAgentToken                        string

	// Messages shown after an action. Most are fmt templates: the verbs are
	// documented where they are used (internal/console/tui.go).
	MsgAgentsFailed, MsgAgentsSaved, MsgAgentsOn string
	MsgRestartFailed, MsgRestartUnavailable      string
	MsgRestarting, MsgRestarted                  string
	MsgLinkFailed, MsgLinkReady, MsgMakingLink   string
	MsgTurningOn, MsgSaved, ResetLinkTitle       string
}

var en = strs{
	Title: "Regulus",

	TabStatus:   "Status",
	TabSettings: "Settings",
	TabLogs:     "Logs",
	TabHealth:   "Health",
	TabActions:  "Actions",

	Running: "running",
	Stopped: "not running",
	Service: "service",

	Hub:          "Hub",
	Version:      "Version",
	Dashboard:    "Dashboard",
	AlsoOn:       "also on",
	AgentPort:    "Agent port",
	BuiltinAgent: "Built-in agent",
	DataDir:      "Data directory",
	Database:     "Database",
	ConfigFile:   "Configuration",
	Uptime:       "up",
	On:           "on",
	Off:          "off",
	None:         "none",
	Agents:       "Connected agents",
	NoAgents:     "No agent is connected.",
	SetupTitle:   "First run",
	SetupText:    "No administrator exists yet. Open the dashboard and enter this one-time setup token:",

	SettingHeader:  [3]string{"Setting", "Value", "Source"},
	Default:        "default",
	AppliesNow:     "Applies as soon as it is saved.",
	AppliesRestart: "The hub reads it at start: restart it to apply.",
	EnvOverride:    "An environment variable overrides the file: saving here will not change what the hub uses.",
	Editing:        "Editing",
	Valid:          "will be saved as",
	Choices:        "choices",

	LogsEmpty:       "No log lines.",
	LogsUnavailable: "Cannot read the log",
	LogFilters:      [4]string{"all levels", "info and above", "warnings and errors", "errors only"},
	Following:       "following",

	Loading: "Loading…",

	HelpMain:    "tab/←/→ section  ↑/↓ move  enter edit  r restart  p reset admin password  q quit",
	HelpEdit:    "enter save  esc cancel  tab next choice",
	HelpConfirm: "y yes  n no",

	RestartAsk:  "Restart the regulus service now?",
	ResetAsk:    "Make a one-time link to choose a new administrator password?",
	RestartHint: "Restart the hub to apply: press r, or run: sudo systemctl restart regulus",

	ActionsIntro:        "Press the key of an action, or select it with ↑/↓ and press enter. Each asks before doing anything.",
	ActRestart:          "Restart the service",
	ActRestartInfo:      "Restarts regulus.service, to apply settings read at start. Needs root or sudo without a password.",
	ActReset:            "Reset the administrator password",
	ActResetInfo:        "Makes a one-time link to choose a new password, for when nobody can sign in. Needs root or the hub's user.",
	ActAgents:           "Turn on remote agents",
	ActAgentsInfo:       "Sets agent_listen to :8443 and restarts the hub, so other servers can connect. Needs root.",
	ActUnavailable:      "not available",
	AgentsAsk:           "Turn on remote agents on port 8443 and restart the hub?",
	NewAgentTitle:       "Adding a server",
	NewAgentText:        "Remote agents are on. On the new server, the installer needs:",
	NewAgentAddress:     "Hub address",
	NewAgentFingerprint: "CA fingerprint",
	NewAgentToken:       "Token: make it in the dashboard, Servers → Add a server (it shows the whole command).",

	MsgAgentsFailed:       "Could not turn on remote agents: %s. Open the console with sudo, or set agent_listen by hand.",
	MsgAgentsSaved:        "Saved agent_listen = %s.",
	MsgAgentsOn:           "Remote agents are on, port 8443. Open it in the firewall towards the servers you manage.",
	MsgRestartFailed:      "Could not restart: %s. Run: sudo systemctl restart %s",
	MsgRestartUnavailable: "Restarting is not available here. Run: sudo systemctl restart %s",
	MsgRestarting:         "Restarting…",
	MsgRestarted:          "The service was restarted.",
	MsgLinkFailed:         "Could not make the link: %s",
	MsgLinkReady:          "Open the link below to choose a new password. It works once.",
	MsgMakingLink:         "Making the link…",
	MsgTurningOn:          "Turning on remote agents…",
	MsgSaved:              "Saved %s = %s.",
	ResetLinkTitle:        "Password reset link",
}

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

// T is the language in use. It is English until UseLocale picks another.
var T = en

// UseLocale picks the console language from the environment, the way the C
// library does: the first of LC_ALL, LC_MESSAGES and LANG that is set decides,
// and a value that starts with "it" (it_IT.UTF-8, it) is Italian. Anything
// else, and no value, is English. It sets the package-wide T, so call it once
// at start (or again in tests: it always resets).
func UseLocale(getenv func(string) string) {
	T = en
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(strings.TrimSpace(getenv(k))); v != "" {
			if v == "it" || strings.HasPrefix(v, "it_") || strings.HasPrefix(v, "it.") || strings.HasPrefix(v, "it@") {
				T = it
			}
			return
		}
	}
}
