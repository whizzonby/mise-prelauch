/** Choices for "how long would you spend cooking", in minutes. */
export const cookTimes = [
  { key: "20", label: "20 minutes" },
  { key: "30", label: "30 minutes" },
  { key: "45", label: "45 minutes" },
  { key: "60", label: "An hour" },
  { key: "90", label: "An hour and a half" },
];

/** Onboarding's "cooking for whom" answers, with the household size each implies. */
export const householdOptions = [
  { key: "1", label: "Just me", size: 1 },
  { key: "2", label: "Two people", size: 2 },
  { key: "family", label: "A small family", size: 4 },
  { key: "large", label: "A larger household", size: 6 },
];
