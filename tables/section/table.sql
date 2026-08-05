CREATE TABLE IF NOT EXISTS section (
    slug VARCHAR(99) NOT NULL,
    year VARCHAR(99) NOT NULL,
    parentSlug VARCHAR(99) NOT NULL DEFAULT "",
    label VARCHAR(199) NOT NULL DEFAULT "",
    sortOrder INT NOT NULL DEFAULT 0,
    PRIMARY KEY (year, slug),
    KEY year_parent (year, parentSlug, sortOrder)
);
