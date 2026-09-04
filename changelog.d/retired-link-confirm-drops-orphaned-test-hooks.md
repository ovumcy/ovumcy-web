none

Two test hooks on the retired OIDC link-confirm page lost their last reader when the browser lane
stopped driving that page, and the DOM-hook guard refuses markup nothing reads. The attributes
carried no behaviour and no styling, so nothing a user can see changes.
