// Copyright 2015 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package variable

import (
	"math"
	"sync"
	"time"

	"github.com/pingcap/tidb/mysql"
	"github.com/pingcap/tidb/terror"
)

const (
	codeCantGetValidID terror.ErrCode = 1
	codeCantSetToNull  terror.ErrCode = 2
)

// Error instances.
var (
	errCantGetValidID = terror.ClassVariable.New(codeCantGetValidID, "cannot get valid auto-increment id in retry")
	ErrCantSetToNull  = terror.ClassVariable.New(codeCantSetToNull, "cannot set variable to null")
)

// RetryInfo saves retry information.
type RetryInfo struct {
	Retrying               bool
	DroppedPreparedStmtIDs []uint32
	currRetryOff           int
	autoIncrementIDs       []int64
}

// Clean does some clean work.
func (r *RetryInfo) Clean() {
	r.currRetryOff = 0
	if len(r.autoIncrementIDs) > 0 {
		r.autoIncrementIDs = r.autoIncrementIDs[:0]
	}
	if len(r.DroppedPreparedStmtIDs) > 0 {
		r.DroppedPreparedStmtIDs = r.DroppedPreparedStmtIDs[:0]
	}
}

// AddAutoIncrementID adds id to AutoIncrementIDs.
func (r *RetryInfo) AddAutoIncrementID(id int64) {
	r.autoIncrementIDs = append(r.autoIncrementIDs, id)
}

// ResetOffset resets the current retry offset.
func (r *RetryInfo) ResetOffset() {
	r.currRetryOff = 0
}

// GetCurrAutoIncrementID gets current AutoIncrementID.
func (r *RetryInfo) GetCurrAutoIncrementID() (int64, error) {
	if r.currRetryOff >= len(r.autoIncrementIDs) {
		return 0, errCantGetValidID
	}
	id := r.autoIncrementIDs[r.currRetryOff]
	r.currRetryOff++

	return id, nil
}

// TransactionContext is used to store variables that has transaction scope.
type TransactionContext struct {
	ForUpdate     bool
	DirtyDB       interface{}
	Binlog        interface{}
	InfoSchema    interface{}
	Histroy       interface{}
	SchemaVersion int64
	UpdateMapper  map[int64]*TableDelta
}

// UpdateDeltaForTable updates the delta info for some table.
func (tc *TransactionContext) UpdateDeltaForTable(tableID int64, delta int64, count int64) {
	if tc.UpdateMapper == nil {
		tc.UpdateMapper = make(map[int64]*TableDelta)
	}
	item, ok := tc.UpdateMapper[tableID]
	if !ok {
		tc.UpdateMapper[tableID] = &TableDelta{delta, count}
	} else {
		item.Delta += delta
		item.Count += count
	}
}

// SessionVars is to handle user-defined or global variables in current session.
type SessionVars struct {
	// user-defined variables
	Users map[string]string
	// system variables
	Systems map[string]string
	// prepared statement
	PreparedStmts        map[uint32]interface{}
	PreparedStmtNameToID map[string]uint32
	// prepared statement auto increment id
	preparedStmtID uint32

	// retry information
	RetryInfo *RetryInfo
	// Should be reset on transaction finished.
	TxnCtx *TransactionContext

	// following variables are special for current session
	Status       uint16
	LastInsertID uint64

	// Client capability
	ClientCapability uint32

	// Connection ID
	ConnectionID uint64

	// Current user
	User string

	// Current DB
	CurrentDB string

	// Strict SQL mode
	StrictSQLMode bool

	// CommonGlobalLoaded indicates if common global variable has been loaded for this session.
	CommonGlobalLoaded bool

	// InRestrictedSQL indicates if the session is handling restricted SQL execution.
	InRestrictedSQL bool

	// SnapshotTS is used for reading history data. For simplicity, SnapshotTS only supports distsql request.
	SnapshotTS uint64

	// SnapshotInfoschema is used with SnapshotTS, when the schema version at snapshotTS less than current schema
	// version, we load an old version schema for query.
	SnapshotInfoschema interface{}

	// GlobalAccessor is used to set and get global variables.
	GlobalVarsAccessor GlobalVarAccessor

	// StmtCtx holds variables for current executing statement.
	StmtCtx *StatementContext

	// AllowAggPushDown can be set to false to forbid aggregation push down.
	AllowAggPushDown bool

	// AllowSubqueryUnFolding can be set to true to fold in subquery
	AllowInSubqueryUnFolding bool

	// CurrInsertValues is used to record current ValuesExpr's values.
	// See http://dev.mysql.com/doc/refman/5.7/en/miscellaneous-functions.html#function_values
	CurrInsertValues interface{}

	// Per-connection time zones. Each client that connects has its own time zone setting, given by the session time_zone variable.
	// See https://dev.mysql.com/doc/refman/5.7/en/time-zone-support.html
	TimeZone *time.Location

	SQLMode mysql.SQLMode

	/* TiDB system variables */

	// SkipConstraintCheck is true when importing data.
	SkipConstraintCheck bool

	// SkipUTF8 check on input value.
	SkipUTF8Check bool

	// SkipDDLWait can be set to true to skip 2 lease wait after create/drop/truncate table, create/drop database.
	// Then if there are multiple TiDB servers, the new table may not be available for other TiDB servers.
	SkipDDLWait bool

	// TiDBBuildStatsConcurrency is used to control statistics building concurrency.
	BuildStatsConcurrencyVar int

	// The number of handles for a index lookup task in index double read executor.
	IndexLookupSize int

	// The number of concurrent index lookup worker.
	IndexLookupConcurrency int

	// The number of concurrent dist SQL scan worker.
	DistSQLScanConcurrency int

	// The number of concurrent index serial scan worker.
	IndexSerialScanConcurrency int

	// Should we split insert data into multiple batches.
	BatchInsert bool
}

// NewSessionVars creates a session vars object.
func NewSessionVars() *SessionVars {
	return &SessionVars{
		Users:                      make(map[string]string),
		Systems:                    make(map[string]string),
		PreparedStmts:              make(map[uint32]interface{}),
		PreparedStmtNameToID:       make(map[string]uint32),
		TxnCtx:                     &TransactionContext{},
		RetryInfo:                  &RetryInfo{},
		StrictSQLMode:              true,
		Status:                     mysql.ServerStatusAutocommit,
		StmtCtx:                    new(StatementContext),
		AllowAggPushDown:           true,
		BuildStatsConcurrencyVar:   DefBuildStatsConcurrency,
		IndexLookupSize:            DefIndexLookupSize,
		IndexLookupConcurrency:     DefIndexLookupConcurrency,
		IndexSerialScanConcurrency: DefIndexSerialScanConcurrency,
		DistSQLScanConcurrency:     DefDistSQLScanConcurrency,
	}
}

const (
	characterSetConnection = "character_set_connection"
	collationConnection    = "collation_connection"
)

// GetCharsetInfo gets charset and collation for current context.
// What character set should the server translate a statement to after receiving it?
// For this, the server uses the character_set_connection and collation_connection system variables.
// It converts statements sent by the client from character_set_client to character_set_connection
// (except for string literals that have an introducer such as _latin1 or _utf8).
// collation_connection is important for comparisons of literal strings.
// For comparisons of strings with column values, collation_connection does not matter because columns
// have their own collation, which has a higher collation precedence.
// See https://dev.mysql.com/doc/refman/5.7/en/charset-connection.html
func (s *SessionVars) GetCharsetInfo() (charset, collation string) {
	charset = s.Systems[characterSetConnection]
	collation = s.Systems[collationConnection]
	return
}

// SetLastInsertID saves the last insert id to the session context.
// TODO: we may store the result for last_insert_id sys var later.
func (s *SessionVars) SetLastInsertID(insertID uint64) {
	s.LastInsertID = insertID
}

// SetStatusFlag sets the session server status variable.
// If on is ture sets the flag in session status,
// otherwise removes the flag.
func (s *SessionVars) SetStatusFlag(flag uint16, on bool) {
	if on {
		s.Status |= flag
		return
	}
	s.Status &= (^flag)
}

// GetStatusFlag gets the session server status variable, returns true if it is on.
func (s *SessionVars) GetStatusFlag(flag uint16) bool {
	return s.Status&flag > 0
}

// InTxn returns if the session is in transaction.
func (s *SessionVars) InTxn() bool {
	return s.GetStatusFlag(mysql.ServerStatusInTrans)
}

// IsAutocommit returns if the session is set to autocommit.
func (s *SessionVars) IsAutocommit() bool {
	return s.GetStatusFlag(mysql.ServerStatusAutocommit)
}

// GetNextPreparedStmtID generates and returns the next session scope prepared statement id.
func (s *SessionVars) GetNextPreparedStmtID() uint32 {
	s.preparedStmtID++
	return s.preparedStmtID
}

// special session variables.
const (
	SQLModeVar          = "sql_mode"
	AutocommitVar       = "autocommit"
	CharacterSetResults = "character_set_results"
	MaxAllowedPacket    = "max_allowed_packet"
	TimeZone            = "time_zone"
)

// TableDelta stands for the changed count for one table.
type TableDelta struct {
	Delta int64
	Count int64
}

// StatementContext contains variables for a statement.
// It should be reset before executing a statement.
type StatementContext struct {
	/* Variables that are set before execution */
	InUpdateOrDeleteStmt bool
	IgnoreTruncate       bool
	TruncateAsWarning    bool
	InShowWarning        bool

	/* Variables that changes during execution. */
	mu struct {
		sync.Mutex
		affectedRows uint64
		foundRows    uint64
		warnings     []error
	}
}

// AddAffectedRows adds affected rows.
func (sc *StatementContext) AddAffectedRows(rows uint64) {
	sc.mu.Lock()
	sc.mu.affectedRows += rows
	sc.mu.Unlock()
}

// AffectedRows gets affected rows.
func (sc *StatementContext) AffectedRows() uint64 {
	sc.mu.Lock()
	rows := sc.mu.affectedRows
	sc.mu.Unlock()
	return rows
}

// FoundRows gets found rows.
func (sc *StatementContext) FoundRows() uint64 {
	sc.mu.Lock()
	rows := sc.mu.foundRows
	sc.mu.Unlock()
	return rows
}

// AddFoundRows adds found rows.
func (sc *StatementContext) AddFoundRows(rows uint64) {
	sc.mu.Lock()
	sc.mu.foundRows += rows
	sc.mu.Unlock()
}

// GetWarnings gets warnings.
func (sc *StatementContext) GetWarnings() []error {
	sc.mu.Lock()
	warns := make([]error, len(sc.mu.warnings))
	copy(warns, sc.mu.warnings)
	sc.mu.Unlock()
	return warns
}

// WarningCount gets warning count.
func (sc *StatementContext) WarningCount() uint16 {
	if sc.InShowWarning {
		return 0
	}
	sc.mu.Lock()
	wc := uint16(len(sc.mu.warnings))
	sc.mu.Unlock()
	return wc
}

// SetWarnings sets warnings.
func (sc *StatementContext) SetWarnings(warns []error) {
	sc.mu.Lock()
	sc.mu.warnings = warns
	sc.mu.Unlock()
}

// AppendWarning appends a warning.
func (sc *StatementContext) AppendWarning(warn error) {
	sc.mu.Lock()
	if len(sc.mu.warnings) < math.MaxUint16 {
		sc.mu.warnings = append(sc.mu.warnings, warn)
	}
	sc.mu.Unlock()
}

// HandleTruncate ignores or returns the error based on the StatementContext state.
func (sc *StatementContext) HandleTruncate(err error) error {
	if err == nil {
		return nil
	}
	if sc.IgnoreTruncate {
		return nil
	}
	if sc.TruncateAsWarning {
		sc.AppendWarning(err)
		return nil
	}
	return err
}

// ResetForRetry resets the changed states during execution.
func (sc *StatementContext) ResetForRetry() {
	sc.mu.Lock()
	sc.mu.affectedRows = 0
	sc.mu.foundRows = 0
	sc.mu.warnings = nil
	sc.mu.Unlock()
}
