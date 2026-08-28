package kernel

type UserID string

func NewUserID(id string) UserID { return UserID(id) }
func (u UserID) String() string  { return string(u) }
func (u UserID) IsEmpty() bool   { return string(u) == "" }

type TenantID string

func NewTenantID(id string) TenantID { return TenantID(id) }
func (t TenantID) String() string    { return string(t) }
func (t TenantID) IsEmpty() bool     { return string(t) == "" }

type InvitationID string

func NewInvitationID(id string) InvitationID { return InvitationID(id) }
func (i InvitationID) String() string        { return string(i) }
func (i InvitationID) IsEmpty() bool         { return string(i) == "" }

type APIKeyID string

func NewAPIKeyID(id string) APIKeyID { return APIKeyID(id) }
func (a APIKeyID) String() string    { return string(a) }
func (a APIKeyID) IsEmpty() bool     { return string(a) == "" }

type RoleID string

func NewRoleID(id string) RoleID { return RoleID(id) }
func (r RoleID) String() string  { return string(r) }
func (r RoleID) IsEmpty() bool   { return string(r) == "" }
