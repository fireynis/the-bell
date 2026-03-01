import { Link } from "react-router";

export default function NotFound() {
  return (
    <div className="p-4 text-center">
      <h1 className="text-2xl font-bold">404</h1>
      <p className="mt-2 text-gray-600">Page not found</p>
      <Link to="/" className="mt-4 inline-block text-blue-600 hover:underline">
        Go home
      </Link>
    </div>
  );
}
