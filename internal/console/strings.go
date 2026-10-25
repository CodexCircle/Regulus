package console

// Every word the console shows that is not data lives here, so that a
// translation replaces one value and touches no other file.
type strs struct {
	Title string

	TabStatus, TabSettings, TabLogs, TabHealth string

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
}

var en = strs{
	Title: "Regulus",

	TabStatus:   "Status",
	TabSettings: "Settings",
	TabLogs:     "Logs",
	TabHealth:   "Health",

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
}

// T is the language in use.
var T = en
