CREATE TABLE records (
                         aggregate_id VARCHAR(255) NOT NULL, -- Aggregate ID
                         version INT NOT NULL,               -- Record version
                         payload BLOB NOT NULL,              -- Record payload
                         PRIMARY KEY (aggregate_id, version) -- Compound primary key
);