-- ==============================================================================
-- Migration 001: Create Checkpoints Table
-- ==============================================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'tps_checkpoints' AND schema_id = SCHEMA_ID('dbo'))
BEGIN
    CREATE TABLE dbo.tps_checkpoints (
        source_name VARCHAR(50) NOT NULL,
        index_pattern VARCHAR(100) NOT NULL,
        last_processed_until DATETIME2(3) NOT NULL,
        last_success_at DATETIME2(3) NOT NULL,
        updated_at DATETIME2(3) NOT NULL CONSTRAINT DF_tps_checkpoints_updated DEFAULT SYSUTCDATETIME(),
        CONSTRAINT PK_tps_checkpoints PRIMARY KEY CLUSTERED (source_name, index_pattern)
    );
END;

