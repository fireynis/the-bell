export interface KratosSession {
  id: string;
  active: boolean;
  identity: KratosIdentity;
  expires_at: string;
  authenticated_at: string;
}

export interface KratosIdentity {
  id: string;
  traits: {
    email: string;
    name?: string;
  };
}

export interface KratosFlow {
  id: string;
  type: string;
  expires_at: string;
  issued_at: string;
  request_url: string;
  ui: UiContainer;
  state?: string;
}

export interface UiContainer {
  action: string;
  method: string;
  nodes: UiNode[];
  messages?: UiText[];
}

export interface UiNode {
  type: string;
  group: string;
  attributes: UiNodeAttributes;
  messages: UiText[];
  meta: UiNodeMeta;
}

export type UiNodeAttributes =
  | UiNodeInputAttributes
  | UiNodeAnchorAttributes
  | UiNodeImageAttributes
  | UiNodeTextAttributes;

export interface UiNodeInputAttributes {
  node_type: "input";
  name: string;
  type: string;
  value?: string;
  required?: boolean;
  disabled?: boolean;
  autocomplete?: string;
  pattern?: string;
  onclick?: string;
}

export interface UiNodeAnchorAttributes {
  node_type: "a";
  href: string;
  title: UiText;
  id: string;
}

export interface UiNodeImageAttributes {
  node_type: "img";
  src: string;
  id: string;
  width: number;
  height: number;
}

export interface UiNodeTextAttributes {
  node_type: "text";
  text: UiText;
  id: string;
}

export interface UiNodeMeta {
  label?: UiText;
}

export interface UiText {
  id: number;
  text: string;
  type: "error" | "info" | "success";
  context?: Record<string, unknown>;
}

export interface KratosLogoutFlow {
  logout_url: string;
  logout_token: string;
}
