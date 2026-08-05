CREATE TABLE IF NOT EXISTS senior (
    memberId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    teamId VARCHAR(99) NOT NULL,
    armNumber VARCHAR(99) NOT NULL DEFAULT '',
    name VARCHAR(99) NOT NULL,
    address VARCHAR(99) NOT NULL,
    postalCode VARCHAR(99) NOT NULL,
    city VARCHAR(99) NOT NULL,
    email VARCHAR(99) NOT NULL,
    phone VARCHAR(99) NOT NULL,
    birthday VARCHAR(99) NOT NULL,
    tshirtSize VARCHAR(9) NOT NULL DEFAULT '',
    diet VARCHAR(9) NOT NULL DEFAULT '',
    createdAt VARCHAR(99) NOT NULL,
    updatedAt VARCHAR(99) NOT NULL,
    PRIMARY KEY (year, memberId)
);
