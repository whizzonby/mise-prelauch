/**
 * Form schemas for the waitlist. These give people fast feedback in the
 * browser; the API validates everything again and is the source of truth
 * (services/api/internal/leads/validate.go). Keep the two in step.
 */
import { z } from "zod";

const NAME_PATTERN = /^[\p{L}\p{M}][\p{L}\p{M}' .-]*$/u;

/** Mirrors NormalizePhone in the API: 7 to 15 digits, optional leading +. */
export function isValidPhone(value: string): boolean {
  const trimmed = value.trim();
  if (!/^\+?[\d\s().-]+$/.test(trimmed)) return false;
  const digits = trimmed.replace(/\D/g, "").length;
  return digits >= 7 && digits <= 15;
}

export const waitlistSchema = z.object({
  firstName: z
    .string()
    .trim()
    .min(1, "Enter your first name.")
    .max(80, "Use 80 characters or fewer.")
    .regex(NAME_PATTERN, "Use letters only, with spaces, hyphens or apostrophes."),
  lastName: z
    .string()
    .trim()
    .min(1, "Enter your last name.")
    .max(80, "Use 80 characters or fewer.")
    .regex(NAME_PATTERN, "Use letters only, with spaces, hyphens or apostrophes."),
  email: z
    .string()
    .trim()
    .min(1, "Enter your email address.")
    .max(254, "That email address is too long.")
    .pipe(z.email("Enter a valid email address, like name@example.com.")),
  phone: z
    .string()
    .trim()
    .refine((v) => v === "" || isValidPhone(v), "Enter a phone number with 7 to 15 digits, or leave it blank."),
  location: z.string().min(1, "Choose where you would like delivery."),
  dietaryInterests: z.array(z.string()).max(12),
  /** "" means the person did not say. */
  householdSize: z.string().regex(/^([1-9]|1[0-2])?$/, "Choose a household size from the list."),
  /** "" means the person did not choose. */
  packagingPreference: z.string(),
  consent: z.boolean().refine((v) => v, "Tick the box so we can email you about the launch."),
});

export type WaitlistValues = z.infer<typeof waitlistSchema>;

export const waitlistDefaults: WaitlistValues = {
  firstName: "",
  lastName: "",
  email: "",
  phone: "",
  location: "",
  dietaryInterests: [],
  householdSize: "",
  packagingPreference: "",
  consent: false,
};

/** Every profiling answer is optional; "" and [] mean skipped. */
export const preferencesSchema = z.object({
  householdType: z.string(),
  mealsPerWeek: z.string().regex(/^(\d{1,2})?$/),
  cookingFrequency: z.string(),
  mealInterests: z.array(z.string()).max(12),
  fitnessGoal: z.string(),
  usage: z.string(),
});

export type PreferencesValues = z.infer<typeof preferencesSchema>;

export const preferencesDefaults: PreferencesValues = {
  householdType: "",
  mealsPerWeek: "",
  cookingFrequency: "",
  mealInterests: [],
  fitnessGoal: "",
  usage: "",
};

export const adminLoginSchema = z.object({
  email: z.string().trim().min(1, "Enter your email address."),
  password: z.string().min(1, "Enter your password."),
});
