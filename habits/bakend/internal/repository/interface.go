package repository

type Data struct {
	Categories []Category `json:"categories"`
	Marks      []Mark     `json:"marks"`
}

type Category struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Mark struct {
	Category string `json:"category"`
	Date     string `json:"date"`
}

type CheckGroup struct {
	Name  string      `json:"name"`
	Items []CheckItem `json:"items"`
}

var userChecks = map[string][]CheckGroup{}

type CheckItem struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
}

type Repository interface {
	GetUserData(user string) (categories []Category, marks []Mark, err error)
	SetCategories(user string, categories []string) error
	ToggleMark(user, category, date string) error
	GetMarksByMonth(user, category, month string) ([]int, error)
	SetCategoryColor(user, category, color string) error
	SetCategoryName(user, oldName, newName, color string) error

	// Checks
	GetChecks(user string) ([]CheckGroup, error)
	AddCheckGroup(user, name string) error
	AddCheckItem(user, group, name string) error
	ToggleCheckItem(user, group, item string, done bool) error
	RenameCheckGroup(user, oldName, newName string) error
	DeleteCheckGroup(user, name string) error
	DeleteCheckItem(user, group, name string) error

	// Metric
	GetMetrics(user string) ([]Metric, error)
	CreateMetric(m Metric) (Metric, error)
	DeleteMetric(id int, user string) error

	GetMetricValues(user string, metricID int, periodDays int) ([]MetricValue, error)
	GetMetricValuesMulti(user string, metricIDs []int, periodDays int) (map[int][]MetricValue, error)
	AddMetricValue(v MetricValue) (MetricValue, error)
	DeleteMetricValue(id int, user string) error
	GetMetricMaxPerDay(metricID int, user string) (int, error)
	CountMetricValuesByDate(metricID int, user, dateStr string) (int, error)

	// Settings
	GetActiveSetting(user, name string) (*UserSetting, error)
	GetAllSettings(user, name string) ([]UserSetting, error)
	SaveSetting(user, name, value, options string) error
	UpdateActiveSetting(user, name, id string) error
	DeleteSetting(user, name, id string) error
	UpsertSetting(user, name, value string) error

	// Diary
	CreateDiaryEntry(user, date, text string) error
	GetDiaryEntries(user, from, to string) ([]DiaryEntry, error)
	SearchDiaryEntries(user, query string) ([]DiaryEntry, error)
	UpdateDiaryEntry(id int, date, text string) error
	DeleteDiaryEntry(id int) error
	GetDiaryEntriesForExport(user, from, to string) ([]DiaryEntry, error)

	// Currencies
	GetUserCurrencies(user string) ([]string, error)
	AddUserCurrency(user, code string) error
	RemoveUserCurrency(user, code string) error
}

type Metric struct {
	ID        int    `json:"id"`
	User      string `json:"user"`
	Name      string `json:"name"`
	MaxPerDay int    `json:"max_per_day"`
	Color     string `json:"color"`
}

type MetricValue struct {
	ID       int     `json:"id"`
	MetricID int     `json:"metric_id"`
	User     string  `json:"user"`
	Datetime string  `json:"datetime"`
	Value    float64 `json:"value"`
}

type UserSetting struct {
	ID      int    `json:"id"`
	User    string `json:"user"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Options string `json:"options"`
	Active  bool   `json:"active"`
}
type DiaryEntry struct {
	ID   int    `json:"id"`
	Date string `json:"date"`
	Text string `json:"text"`
}
