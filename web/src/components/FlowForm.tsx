import type { FormEvent } from "react";
import type { KratosFlow, UiNode, UiNodeInputAttributes, UiText } from "../api/kratos-types.ts";

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
          className={
            m.type === "error"
              ? "text-sm text-red-600"
              : m.type === "success"
                ? "text-sm text-green-600"
                : "text-sm text-blue-600"
          }
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
          className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:outline-none disabled:opacity-50"
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
        <label htmlFor={attrs.name} className="mb-1 block text-sm font-medium text-gray-700">
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
        className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none disabled:bg-gray-50"
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

const VISIBLE_GROUPS = new Set(["default", "password", "code", "link"]);

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

  // Separate hidden, visible inputs, and submit buttons for layout
  const hidden = nodes.filter(
    (n) => (n.attributes as UiNodeInputAttributes).node_type === "input" && (n.attributes as UiNodeInputAttributes).type === "hidden",
  );
  const inputs = nodes.filter(
    (n) =>
      (n.attributes as UiNodeInputAttributes).node_type === "input" &&
      (n.attributes as UiNodeInputAttributes).type !== "hidden" &&
      (n.attributes as UiNodeInputAttributes).type !== "submit" &&
      (n.attributes as UiNodeInputAttributes).type !== "button",
  );
  const submits = nodes.filter(
    (n) =>
      (n.attributes as UiNodeInputAttributes).node_type === "input" &&
      ((n.attributes as UiNodeInputAttributes).type === "submit" || (n.attributes as UiNodeInputAttributes).type === "button"),
  );

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <MessageList messages={flow.ui.messages} />
      {hidden.map(renderNode)}
      {inputs.map(renderNode)}
      {submitting ? (
        <div className="flex justify-center py-2">
          <div className="h-5 w-5 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" />
        </div>
      ) : (
        submits.map(renderNode)
      )}
    </form>
  );
}
