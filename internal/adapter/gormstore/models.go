package gormstore

type schemaVersionRow struct {
	Version int `gorm:"column:version;primaryKey"`
}

func (schemaVersionRow) TableName() string { return "schema_version" }

type draftRow struct {
	Settings string `gorm:"column:settings;type:TEXT;not null"`
	ID       int    `gorm:"column:id;primaryKey;autoIncrement:false;check:chk_draft_singleton,id = 1"`
	Revision int64  `gorm:"column:revision;not null"`
	Services string `gorm:"column:services;type:TEXT;not null"`
}

func (draftRow) TableName() string { return "draft" }

type draftRevisionRow struct {
	Settings string `gorm:"column:settings;type:TEXT;not null"`
	Revision int64  `gorm:"column:revision;primaryKey;autoIncrement:false"`
	Services string `gorm:"column:services;type:TEXT;not null"`
}

func (draftRevisionRow) TableName() string { return "draft_revisions" }

type adminRow struct {
	ID         int    `gorm:"column:id;primaryKey;autoIncrement:false;check:chk_admin_singleton,id = 1"`
	Username   string `gorm:"column:username;type:TEXT;not null"`
	Password   string `gorm:"column:password;type:TEXT;not null"`
	MustChange bool   `gorm:"column:must_change;type:INTEGER;not null;default:0"`
}

func (adminRow) TableName() string { return "admin" }

type sessionRow struct {
	Hash    string `gorm:"column:hash;type:TEXT;primaryKey"`
	CSRF    string `gorm:"column:csrf;type:TEXT;not null"`
	Expires int64  `gorm:"column:expires;not null"`
}

func (sessionRow) TableName() string { return "sessions" }

type validationRow struct {
	Settings   string `gorm:"column:settings;type:TEXT;not null"`
	ID         string `gorm:"column:id;type:TEXT;primaryKey"`
	Revision   int64  `gorm:"column:revision;not null"`
	Config     []byte `gorm:"column:config;type:BLOB;not null"`
	Services   string `gorm:"column:services;type:TEXT;not null"`
	BaseHash   string `gorm:"column:base_hash;type:TEXT;not null"`
	PolicyHash string `gorm:"column:policy_hash;type:TEXT;not null"`
	RollbackID string `gorm:"column:rollback_id;type:TEXT;not null"`
	Created    int64  `gorm:"column:created;not null"`
}

func (validationRow) TableName() string { return "validations" }

type deploymentRow struct {
	Settings    string `gorm:"column:settings;type:TEXT;not null"`
	ID          string `gorm:"column:id;type:TEXT;primaryKey"`
	Version     int64  `gorm:"column:version;not null;uniqueIndex"`
	Revision    int64  `gorm:"column:revision;not null"`
	Status      string `gorm:"column:status;type:TEXT;not null;index:one_pending_deployment,unique,expression:(1),where:status = 'applying' OR status = 'uncertain'"`
	Config      []byte `gorm:"column:config;type:BLOB;not null"`
	Services    string `gorm:"column:services;type:TEXT;not null"`
	BaseHash    string `gorm:"column:base_hash;type:TEXT;not null"`
	Hash        string `gorm:"column:hash;type:TEXT;not null"`
	Actor       string `gorm:"column:actor;type:TEXT;not null"`
	Created     string `gorm:"column:created;type:TEXT;not null"`
	Finished    string `gorm:"column:finished;type:TEXT;not null;default:''"`
	Error       string `gorm:"column:error;type:TEXT;not null;default:''"`
	RollbackID  string `gorm:"column:rollback_id;type:TEXT;not null;default:''"`
	Changes     string `gorm:"column:changes;type:TEXT;not null"`
	Idempotency string `gorm:"column:idempotency;type:TEXT;not null;uniqueIndex"`
	RequestHash string `gorm:"column:request_hash;type:TEXT;not null"`
}

func (deploymentRow) TableName() string { return "deployments" }

type auditRow struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Time       string `gorm:"column:time;type:TEXT;not null"`
	Actor      string `gorm:"column:actor;type:TEXT;not null"`
	Action     string `gorm:"column:action;type:TEXT;not null"`
	Object     string `gorm:"column:object;type:TEXT;not null"`
	Result     string `gorm:"column:result;type:TEXT;not null"`
	Revision   int64  `gorm:"column:revision;not null"`
	Version    int64  `gorm:"column:version;not null"`
	RollbackID string `gorm:"column:rollback_id;type:TEXT;not null;default:''"`
	ErrorClass string `gorm:"column:error_class;type:TEXT;not null;default:''"`
}

func (auditRow) TableName() string { return "audit" }

type loginLimitRow struct {
	Key   string `gorm:"column:key;type:TEXT;primaryKey"`
	Count int    `gorm:"column:count;not null"`
	Reset int64  `gorm:"column:reset;not null"`
}

func (loginLimitRow) TableName() string { return "login_limits" }

type managedSettingsRow struct {
	ID    int    `gorm:"column:id;primaryKey;autoIncrement:false;check:chk_managed_settings_singleton,id = 1"`
	Value string `gorm:"column:value;type:TEXT;not null"`
}

func (managedSettingsRow) TableName() string { return "managed_settings" }

type certificateStateRow struct {
	ID               int    `gorm:"column:id;primaryKey;autoIncrement:false;check:chk_certificate_state_singleton,id = 1"`
	Mode             string `gorm:"column:mode;type:TEXT;not null;check:chk_certificate_state_mode,mode = 'bootstrap_internal' OR mode = 'cloudflare'"`
	ActivationStatus string `gorm:"column:activation_status;type:TEXT;not null;check:chk_certificate_state_activation,activation_status = 'idle' OR activation_status = 'applying' OR activation_status = 'uncertain' OR activation_status = 'failed' OR activation_status = 'success'"`
	PublicStatus     string `gorm:"column:public_status;type:TEXT;not null;check:chk_certificate_state_public,public_status = 'unknown' OR public_status = 'pending' OR public_status = 'ready' OR public_status = 'error'"`
	LastErrorClass   string `gorm:"column:last_error_class;type:TEXT;not null;default:''"`
	UpdatedAt        string `gorm:"column:updated_at;type:TEXT;not null"`
	BeforeHash       string `gorm:"column:before_hash;type:TEXT;not null;default:''"`
	CandidateHash    string `gorm:"column:candidate_hash;type:TEXT;not null;default:''"`
}

func (certificateStateRow) TableName() string { return "certificate_state" }
