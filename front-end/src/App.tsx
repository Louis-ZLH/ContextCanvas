import { createBrowserRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { Toaster } from "react-hot-toast";
import Graph from "./view/graph";
import { loader as GraphLoader } from "./view/graph";
import GraphLayout from "./ui/layout/GraphLayout";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 60 * 1000,
    },
  },
});

const router = createBrowserRouter([
  {
    path: "/",
    element: <div>Hello World</div>,
  },
  {
    path: "/about",
    element: <div>About</div>,
  },
  {
    path: "/graph",
    element: <GraphLayout />,
    children: [
      {
        path: ":graph_id",
        loader: GraphLoader,
        element: <Graph />,
      },
      {
        index: true,
        element: <div>Create Graph</div>,
      },
    ],
  },
]);

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ReactQueryDevtools initialIsOpen={false}></ReactQueryDevtools>
      <RouterProvider router={router} />
      <Toaster
        position="top-center"
        gutter={20}
        containerStyle={{
          margin: "8px",
        }}
        toastOptions={{
          success: {
            duration: 3000,
          },
          error: {
            duration: 5000,
          },
        }}
      />
    </QueryClientProvider>
  );
}

export default App;
