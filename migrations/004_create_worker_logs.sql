-- ==============================================================================
-- Migration 004: Create Operational Worker Logs Table
-- ==============================================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'tps_worker_logs' AND schema_id = SCHEMA_ID('dbo'))
BEGIN
    CREATE TABLE dbo.tps_worker_logs (
        log_id BIGINT IDENTITY(1,1) NOT NULL CONSTRAINT PK_tps_worker_logs PRIMARY KEY CLUSTERED,
        source_name VARCHAR(50) NOT NULL,
        event_type VARCHAR(50) NOT NULL, -- 'POLL_SUCCESS', 'POLL_ERROR', 'DB_ERROR', 'RECOVERY'
        message NVARCHAR(1000) NOT NULL,
        lag_seconds INT NULL,
        created_at DATETIME2(3) NOT NULL CONSTRAINT DF_tps_worker_logs_created DEFAULT SYSUTCDATETIME()
    );
END;

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_tps_worker_logs_source_time' AND object_id = OBJECT_ID('dbo.tps_worker_logs'))
BEGIN
    CREATE NONCLUSTERED INDEX IX_tps_worker_logs_source_time 
    ON dbo.tps_worker_logs (source_name, created_at);
END;

