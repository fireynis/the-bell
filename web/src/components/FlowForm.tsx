import type { FormEvent } from "react";
import type { KratosFlow, UiNode, UiNodeInputAttributes, UiText } from "../api/kratos-types.ts";
import Spinner from "./Spinner.tsx";

interface FlowFormProps {
  flow: KratosFlow;
  onSubmit: (values: Record<string, unknown>) => void;
  submitting?: boolean;
}

function MessageList({ messages }: { messages?: UiText[] }) {
  if (!messages?.length) return null;
  return (
    <div className="space-y-1">
      {messages.map((m) => (
        <p
          key={m.id}
          className="text-sm"
          style={{
            color:
              m.type === "error"
                ? "var(--color-danger)"
                : m.type === "success"
                  ? "var(--color-success)"
                  : "var(--color-info)",
          }}
        >
          {m.text}
        </p>
      ))}
    </div>
  );
}

function InputNode({
  node,
}: {
  node: UiNode;
}) {
  const attrs = node.attributes as UiNodeInputAttributes;

  if (attrs.type === "hidden") {
    return <input type="hidden" name={attrs.name} value={attrs.value ?? ""} />;
  }

  if (attrs.type === "submit" || attrs.type === "button") {
    return (
      <div>
        <button
          type="submit"
          name={attrs.name}
          value={attrs.value ?? ""}
          disabled={attrs.disabled}
          className="w-full rounded-[var(--radius-sm)] px-4 py-2 text-sm font-medium focus:ring-2 focus:ring-offset-2 focus:outline-none disabled:opacity-50"
          style={{
            backgroundColor: "var(--color-primary)",
            color: "var(--color-text-inverse)",
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.backgroundColor = "var(--color-primary-hover)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.backgroundColor = "var(--color-primary)";
          }}
        >
          {node.meta.label?.text ?? "Submit"}
        </button>
        <MessageList messages={node.messages} />
      </div>
    );
  }

  const label = node.meta.label?.text;

  return (
    <div>
      {label && (
        <label
          htmlFor={attrs.name}
          className="mb-1 block text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          {label}
        </label>
      )}
      <input
        id={attrs.name}
        name={attrs.name}
        type={attrs.type}
        defaultValue={attrs.value ?? ""}
        required={attrs.required}
        disabled={attrs.disabled}
        autoComplete={attrs.autocomplete}
        pattern={attrs.pattern}
        className="block w-full rounded-[var(--radius-sm)] px-3 py-2 text-sm shadow-sm focus:outline-none disabled:opacity-50"
        style={{
          borderWidth: "1px",
          borderStyle: "solid",
          borderColor: "var(--color-border)",
          color: "var(--color-text)",
          backgroundColor: "var(--color-surface)",
        }}
        onFocus={(e) => {
          e.currentTarget.style.borderColor = "var(--color-primary)";
          e.currentTarget.style.boxShadow = "0 0 0 1px var(--color-primary)";
        }}
        onBlur={(e) => {
          e.currentTarget.style.borderColor = "var(--color-border)";
          e.currentTarget.style.boxShadow = "none";
        }}
      />
      <MessageList messages={node.messages} />
    </div>
  );
}

function renderNode(node: UiNode) {
  const attrs = node.attributes;

  switch (attrs.node_type) {
    case "input":
      return <InputNode key={`${(attrs as UiNodeInputAttributes).name}-${node.group}`} node={node} />;
    case "a":
      return null; // anchor nodes handled separately if needed
    case "img":
      return null;
    case "text":
      return null;
    default:
      return null;
  }
}

const VISIBLE_GROUPS = new Set(["default", "password", "profile", "code", "link"]);

export default function FlowForm({ flow, onSubmit, submitting }: FlowFormProps) {
  const nodes = flow.ui.nodes.filter((n) => VISIBLE_GROUPS.has(n.group));

  const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);

    // Include the submit button's name/value
    const submitter = (e.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    if (submitter?.name) {
      data.set(submitter.name, submitter.value);
    }

    const values: Record<string, unknown> = {};
    data.forEach((v, k) => {
      values[k] = v;
    });

    onSubmit(values);
  };

  // Narrow to input nodes first, then partition by input type
  const inputNodes = nodes.filter(
    (n): n is UiNode & { attributes: UiNodeInputAttributes } =>
      n.attributes.node_type === "input",
  );
  const hidden = inputNodes.filter((n) => n.attributes.type === "hidden");
  const inputs = inputNodes.filter(
    (n) => n.attributes.type !== "hidden" && n.attributes.type !== "submit" && n.attributes.type !== "button",
  );
  const submits = inputNodes.filter(
    (n) => n.attributes.type === "submit" || n.attributes.type === "button",
  );

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <MessageList messages={flow.ui.messages} />
      {hidden.map(renderNode)}
      {inputs.map(renderNode)}
      {submitting ? (
        <div className="flex justify-center py-2">
          <Spinner />
        </div>
      ) : (
        submits.map(renderNode)
      )}
    </form>
  );
}
