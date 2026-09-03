CREATE TABLE IF NOT EXISTS spejder (
    memberId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    teamId VARCHAR(99) NOT NULL,
    name VARCHAR(99) NOT NULL,
    address VARCHAR(99) NOT NULL,
    postalCode VARCHAR(99) NOT NULL,
    city VARCHAR(99) NOT NULL,
    email VARCHAR(99) NOT NULL,
    phone VARCHAR(99) NOT NULL,
    phoneParent VARCHAR(99) NOT NULL,
    birthday VARCHAR(99) NOT NULL,
    tshirtSize VARCHAR(9) NOT NULL DEFAULT '',
    `returning` TINYINT NOT NULL,
    createdAt VARCHAR(99) NOT NULL,
    updatedAt VARCHAR(99) NOT NULL,
    PRIMARY KEY (year, memberId),
    -- Read by teamId (member count, t-shirt count), which the PK (year, memberId)
    -- cannot serve. teamId leads so it also serves GROUP BY teamId in the patrol
    -- list's pre-grouped member aggregate; year trails to keep per-year lookups
    -- index-only.
    KEY idx_spejder_team (teamId, year)
);
