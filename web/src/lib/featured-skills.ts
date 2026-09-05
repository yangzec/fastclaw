export type FeaturedSkill = {
  name: string;
  label: string;
  blurb: string;
};

/** Skills a person can install without knowing skills.sh exists. */
export const FEATURED_SKILLS: FeaturedSkill[] = [
  { name: "web-search", label: "Web search", blurb: "Look things up and open a page." },
  { name: "translation", label: "Translation", blurb: "Translate between languages." },
  { name: "data-analysis", label: "Data analysis", blurb: "Read a table and say what it means." },
];
