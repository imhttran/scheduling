"use client";

import { type FormEvent } from "react";
import { submitAuthedForm } from "@/lib/api";
import { COUNTRIES, US_STATES } from "@/lib/usStates";
import { PageTitle } from "@/components/PageTitle";

export default function ProfilePage() {
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    void submitAuthedForm("/api/profile", {
      firstName: data.get("firstName"),
      lastName: data.get("lastName"),
      address: data.get("address"),
      address2: data.get("address2"),
      state: data.get("state"),
      zip: data.get("zip"),
      country: data.get("country"),
      phone: data.get("phone"),
      communicationPreference: data.get("communicationPreference"),
      linkedin: data.get("linkedin"),
      github: data.get("github"),
      altEmail: data.get("altEmail"),
    });
  };

  return (
    <div className="login-container">
      <PageTitle title="Complete Your Profile | Frontend Template" />
      <form className="login-form" onSubmit={handleSubmit}>
        <h1>Complete Your Profile</h1>
        <p>Tell us a bit about yourself before continuing</p>

        <div className="input-group">
          <label htmlFor="first-name">First Name</label>
          <input type="text" id="first-name" name="firstName" required />
        </div>

        <div className="input-group">
          <label htmlFor="last-name">Last Name</label>
          <input type="text" id="last-name" name="lastName" required />
        </div>

        <div className="input-group">
          <label htmlFor="address">Address</label>
          <input
            type="text"
            id="address"
            name="address"
            title="Street address, P.O. box, company name, c/o"
            required
          />
        </div>

        <div className="input-group">
          <label htmlFor="address2">Address 2 (optional)</label>
          <input
            type="text"
            id="address2"
            name="address2"
            title="Apartment, suite, unit, building, floor, etc."
          />
        </div>

        <div className="input-group">
          <label htmlFor="state">State</label>
          <select id="state" name="state" required>
            {US_STATES.map(([code, name]) => (
              <option key={code} value={code}>
                {name}
              </option>
            ))}
          </select>
        </div>

        <div className="input-group">
          <label htmlFor="zip">Zip Code</label>
          <input
            type="text"
            id="zip"
            name="zip"
            inputMode="numeric"
            pattern="[0-9]{5}"
            title="5-digit US zip code"
            required
          />
        </div>

        <div className="input-group">
          <label htmlFor="country">Country</label>
          <select id="country" name="country" required>
            {COUNTRIES.map(([code, name]) => (
              <option key={code} value={code}>
                {name}
              </option>
            ))}
          </select>
        </div>

        <div className="input-group">
          <label htmlFor="phone">Phone / Text Number</label>
          <input
            type="tel"
            id="phone"
            name="phone"
            placeholder="(555) 555-5555"
            title="US phone number, e.g. (555) 555-5555"
            required
          />
        </div>

        <div className="input-group">
          <label htmlFor="communication-preference">
            Preferred Contact Method
          </label>
          <select
            id="communication-preference"
            name="communicationPreference"
            required
          >
            <option value="email">Email</option>
            <option value="text">Text</option>
            <option value="phone">Phone</option>
          </select>
        </div>

        <div className="input-group">
          <label htmlFor="linkedin">LinkedIn Profile (optional)</label>
          <input
            type="url"
            id="linkedin"
            name="linkedin"
            placeholder="https://linkedin.com/in/..."
            title="Full URL to your LinkedIn profile"
          />
        </div>

        <div className="input-group">
          <label htmlFor="github">GitHub Profile (optional)</label>
          <input
            type="url"
            id="github"
            name="github"
            placeholder="https://github.com/..."
            title="Full URL to your GitHub profile"
          />
        </div>

        <div className="input-group">
          <label htmlFor="alt-email">Additional Email (optional)</label>
          <input
            type="email"
            id="alt-email"
            name="altEmail"
            title="A second email address you'd like on file, if different from your login email"
          />
        </div>

        <button type="submit" className="login-button">
          Save Profile
        </button>
      </form>
    </div>
  );
}
