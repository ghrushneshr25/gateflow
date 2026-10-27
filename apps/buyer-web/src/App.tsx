import { useState } from "react";
import { Link, Route, Routes } from "react-router-dom";
import { BuyerAuthBar } from "./auth/BuyerAuthBar";
import { BookingPage } from "./pages/BookingPage";
import { OfferingDetailPage } from "./pages/OfferingDetailPage";
import { OfferingsPage } from "./pages/OfferingsPage";
import { OrderDetailPage } from "./pages/OrderDetailPage";
import { OrdersPage } from "./pages/OrdersPage";

export function App() {
  const [, setTick] = useState(0);
  return (
    <div className="shell">
      <header>
        <span className="badge">Buyer Web · P5.3.0</span>
        <h1>Gateflow</h1>
        <p className="muted">
          Discover → queue → grant → checkout. Identity is buyer JWT only (no X-Buyer-ID).
        </p>
        <nav aria-label="Primary">
          <Link to="/">Home</Link>
          <Link to="/offerings">Offerings</Link>
          <Link to="/orders">Orders</Link>
        </nav>
      </header>
      <BuyerAuthBar onChange={() => setTick((n) => n + 1)} />
      <main className="card">
        <Routes>
          <Route
            path="/"
            element={
              <div>
                <h2>Welcome</h2>
                <p className="muted">
                  Sign up with Gmail or email, browse offerings, join the queue, get a grant, then
                  checkout. Confirmed only when the order API returns CONFIRMED.
                </p>
                <p>
                  <Link to="/offerings">Browse offerings</Link>
                </p>
              </div>
            }
          />
          <Route path="/offerings" element={<OfferingsPage />} />
          <Route path="/offerings/:offeringId" element={<OfferingDetailPage />} />
          <Route path="/book/:offeringId" element={<BookingPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/orders/:orderId" element={<OrderDetailPage />} />
        </Routes>
      </main>
    </div>
  );
}
