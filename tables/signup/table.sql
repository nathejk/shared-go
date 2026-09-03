CREATE TABLE IF NOT EXISTS signup (
    teamId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    teamType VARCHAR(99) NOT NULL,
    name VARCHAR(99) NOT NULL,
    emailPending VARCHAR(99) NOT NULL,
    email VARCHAR(99),
	phonePending VARCHAR(99) NOT NULL,
	phone VARCHAR(99),
	pincode VARCHAR(9),
	secret VARCHAR(99),
	createdAt VARCHAR(99),
    PRIMARY KEY (teamId),
    -- internal/data/signup.go reads by (year, teamType); the PK (teamId) cannot.
    KEY idx_signup_year_type (year, teamType)
);
