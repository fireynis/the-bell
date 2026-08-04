import { Link } from "react-router";
import type { Vouch } from "../../api/types";

/** Which side of the vouch relationship this list is showing. */
export type VouchDirection = "received" | "given";

const HEADINGS: Record<VouchDirection, string> = {
  received: "Received",
  given: "Given",
};

/**
 * counterpartId returns the other party in a vouch: whoever vouched for this
 * profile in a received vouch, and whoever this profile vouched for in a given
 * one. Listing the profile's own id would make every row link back to the page
 * it is already on.
 */
function counterpartId(vouch: Vouch, direction: VouchDirection): string {
  return direction === "received" ? vouch.voucher_id : vouch.vouchee_id;
}

/**
 * VouchList renders one side of a profile's vouches.
 *
 * The direction is a prop rather than something inferred from the heading. It
 * used to be derived by comparing the heading text to the literal "Received",
 * so renaming the heading — a change any designer would consider cosmetic —
 * would have flipped every row to link and label the wrong user, with the list
 * still rendering perfectly and nothing failing.
 */
export default function VouchList({
  direction,
  vouches,
}: {
  direction: VouchDirection;
  vouches: Vouch[];
}) {
  const heading = HEADINGS[direction];

  return (
    <div>
      <h3
        className="mb-2 text-sm font-medium"
        style={{ color: "var(--color-text-secondary)" }}
      >
        {heading}
      </h3>
      {vouches.length === 0 ? (
        <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          None yet.
        </p>
      ) : (
        <ul className="space-y-2">
          {vouches.map((v) => {
            const otherId = counterpartId(v, direction);
            return (
              <li
                key={v.id}
                className="rounded-md p-3"
                style={{
                  backgroundColor: "var(--color-surface)",
                  boxShadow: "var(--shadow-sm)",
                }}
              >
                <div className="flex items-center justify-between">
                  <Link
                    to={`/profile/${otherId}`}
                    className="text-sm font-medium"
                    style={{ color: "var(--color-primary)" }}
                  >
                    {otherId.slice(0, 8)}...
                  </Link>
                  <span
                    className="text-xs"
                    style={{ color: "var(--color-text-tertiary)" }}
                  >
                    {new Date(v.created_at).toLocaleDateString()}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
