CREATE TABLE IF NOT EXISTS crewmember (
    userId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    name VARCHAR(199) NOT NULL DEFAULT "",
    phone VARCHAR(99) NOT NULL DEFAULT "",
    email VARCHAR(199) NOT NULL DEFAULT "",
    medlemNr VARCHAR(99) NOT NULL DEFAULT "",
    groupName VARCHAR(99) NOT NULL DEFAULT "",
    corps VARCHAR(99) NOT NULL DEFAULT "",
    diet VARCHAR(199) NOT NULL DEFAULT "",
    additionals TEXT NOT NULL,
    sectionSlug VARCHAR(99) NOT NULL DEFAULT "",
    deleted TINYINT(1) NOT NULL DEFAULT 0,
    PRIMARY KEY (userId),
    KEY year_section (year, sectionSlug)
);
