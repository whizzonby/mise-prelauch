"use client";

import type { Address, Term } from "@mise/api-client";

import { CheckboxField, SelectField, TextField } from "./forms";

/**
 * The fields of a delivery address, for adding and editing. Ids are prefixed
 * so several address forms can sit on one page.
 */
export function AddressFields({ prefix, regions, address }: { prefix: string; regions: Term[]; address?: Address }) {
  const id = (name: string) => `${prefix}-${name}`;
  return (
    <>
      <TextField id={id("label")} name="label" label="Label" optional maxLength={40} defaultValue={address?.label ?? ""} hint="For example Home or Work." />
      <TextField id={id("recipient_name")} name="recipient_name" label="Who receives deliveries" autoComplete="name" required maxLength={120} defaultValue={address?.recipient_name ?? ""} />
      <TextField id={id("phone")} name="phone" label="Phone for the driver" type="tel" autoComplete="tel" optional defaultValue={address?.phone ?? ""} />
      <TextField id={id("line1")} name="line1" label="Street address" autoComplete="address-line1" required maxLength={120} defaultValue={address?.line1 ?? ""} />
      <TextField id={id("line2")} name="line2" label="Apartment, building or landmark" autoComplete="address-line2" optional maxLength={120} defaultValue={address?.line2 ?? ""} />
      <div className="grid gap-5 sm:grid-cols-2">
        <TextField id={id("area")} name="area" label="Area or neighbourhood" optional maxLength={80} defaultValue={address?.area ?? ""} />
        <TextField id={id("city")} name="city" label="Town or city" autoComplete="address-level2" required maxLength={80} defaultValue={address?.city ?? ""} />
      </div>
      <SelectField id={id("region")} name="region" label="Region" options={regions} defaultValue={address?.region ?? ""} placeholder="Choose a region" />
      <p className="type-caption -mt-2 text-muted">Country: Trinidad and Tobago</p>
      <TextField
        id={id("instructions")}
        name="instructions"
        label="Delivery instructions"
        optional
        maxLength={300}
        defaultValue={address?.instructions ?? ""}
        hint="Gate codes, where to leave the box, the best time to call."
      />
      {address?.is_default ? (
        <input type="hidden" name="was_default" value="true" />
      ) : (
        <CheckboxField id={id("is_default")} name="is_default" label="Make this my default address" />
      )}
    </>
  );
}
