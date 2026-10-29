package console

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
}

// T is the language in use.
var T = en
