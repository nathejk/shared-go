CREATE TABLE IF NOT EXISTS klan (
    teamId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL DEFAULT "",
    name VARCHAR(99) NOT NULL DEFAULT "",
    groupName VARCHAR(99) NOT NULL DEFAULT "",
    korps VARCHAR(9) NOT NULL DEFAULT "",
    memberCount INT NOT NULL DEFAULT 0,
    requestedMemberCount INT NOT NULL DEFAULT 0,
    reservedMemberCount INT NOT NULL DEFAULT 0,
    signupStatus VARCHAR(19) NOT NULL DEFAULT "",
    lok VARCHAR(9) NOT NULL DEFAULT "",
    PRIMARY KEY (teamId)
);
