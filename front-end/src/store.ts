import { configureStore } from "@reduxjs/toolkit";
import userReducer from "./feature/user/userSlice.ts";

export const store = configureStore({
  reducer: {
    user: userReducer,
  },
});

export default store;
