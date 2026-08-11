import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import OwnMuteNotice from "./OwnMuteNotice";
import type { User } from "../api/types";

/**
 * A lifted mute used to leave a member with nothing: muted_until disappears the
 * instant a moderator releases them, and the moderation audit trail that would
 * have explained it sits entirely behind the moderator-only /v1/moderation
 * routes. These pin the half a member can actually see.
 */

function member(overrides: Partial<User> = {}): User {
  return {
    id: "user-1",
    display_name: "Ada",
    bio: "",
    avatar_url: "",
    trust_score: 60,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("OwnMuteNotice", () => {
  it("renders nothing for a member who has never been moderated", () => {
    // Not an empty panel: a member with a clean record should not be shown a
    // moderation section of their own profile at all.
    const { container } = render(<OwnMuteNotice user={member()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("tells a released member that their mute was lifted", () => {
    render(
      <OwnMuteNotice
        user={member({
          mute_lifts: [
            {
              lifted_at: "2026-03-01T12:00:00Z",
              previous_muted_until: "2099-03-02T14:00:00Z",
            },
          ],
        })}
      />,
    );

    expect(screen.getByTestId("own-mute-notice")).toBeTruthy();
    expect(screen.getByText(/lifted a mute on your account/i)).toBeTruthy();
  });

  it("says what the lifted mute would otherwise have run to", () => {
    render(
      <OwnMuteNotice
        user={member({
          mute_lifts: [
            {
              lifted_at: "2026-03-01T12:00:00Z",
              previous_muted_until: "2099-03-02T14:00:00Z",
            },
          ],
        })}
      />,
    );

    expect(screen.getByText(/would otherwise have run until/i)).toBeTruthy();
  });

  it("omits the original end time when the record has none", () => {
    render(
      <OwnMuteNotice user={member({ mute_lifts: [{ lifted_at: "2026-03-01T12:00:00Z" }] })} />,
    );

    expect(screen.getByText(/lifted a mute on your account/i)).toBeTruthy();
    expect(screen.queryByText(/would otherwise have run until/i)).toBeNull();
  });

  it("shows an active mute and says posting is blocked", () => {
    render(<OwnMuteNotice user={member({ muted_until: "2099-08-12T12:00:00Z" })} />);

    expect(screen.getByText(/You are muted until/i)).toBeTruthy();
    expect(screen.getByText(/cannot post until then/i)).toBeTruthy();
  });

  it("shows both when a member was released once and is muted again", () => {
    // Two different facts. Being muted again does not undo the earlier release,
    // and showing only the current mute would hide half the record.
    render(
      <OwnMuteNotice
        user={member({
          muted_until: "2099-08-12T12:00:00Z",
          mute_lifts: [{ lifted_at: "2026-03-01T12:00:00Z" }],
        })}
      />,
    );

    expect(screen.getByText(/You are muted until/i)).toBeTruthy();
    expect(screen.getByText(/lifted a mute on your account/i)).toBeTruthy();
  });

  it("treats an expired mute as no mute", () => {
    // The server omits an expired muted_until, but a page left open outlives
    // the response that loaded it and must not go on saying "you cannot post".
    const { container } = render(
      <OwnMuteNotice user={member({ muted_until: "2020-01-01T00:00:00Z" })} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("never names the moderator who acted", () => {
    // Which moderator acted is on no member-facing response; making it visible
    // is a policy decision, not a side effect of this notice.
    render(
      <OwnMuteNotice
        user={member({
          mute_lifts: [{ lifted_at: "2026-03-01T12:00:00Z" }],
        })}
      />,
    );

    expect(screen.getByTestId("own-mute-notice").textContent).not.toMatch(/moderator-\w|mod-\d/i);
  });
});
