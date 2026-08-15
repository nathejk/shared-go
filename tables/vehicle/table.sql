CREATE TABLE IF NOT EXISTS vehicle (
    vehicleId VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    licensePlate VARCHAR(99) NOT NULL DEFAULT "",
    custodianUserId VARCHAR(99) NOT NULL DEFAULT "",
    driverUserId VARCHAR(99) NOT NULL DEFAULT "",
    sectionSlug VARCHAR(99) NOT NULL DEFAULT "",
    color VARCHAR(99) NOT NULL DEFAULT "",
    brand VARCHAR(99) NOT NULL DEFAULT "",
    model VARCHAR(99) NOT NULL DEFAULT "",
    seatCount INT NOT NULL DEFAULT 0,
    description TEXT NOT NULL,
    deleted TINYINT(1) NOT NULL DEFAULT 0,
    PRIMARY KEY (vehicleId),
    KEY year_driver (year, driverUserId),
    KEY year_section (year, sectionSlug)
);
