### Security

- **Changing `SECRET_KEY` and then changing it back no longer brings an old calendar link back to
  life.** A key change refused every armed calendar link but left its row in place, so returning to
  the earlier key made a leaked subscribe URL answer again. The first start under a changed key now
  clears every armed link, whatever its age, and each owner generates a fresh one from Settings.
- **Generating or rotating a calendar link can no longer end with the old link dead and the new
  one never shown.** The one-time reveal is now prepared before the new link is saved, so a failure
  there leaves the previous link working. The reveal is also tied to the link it was prepared for:
  a reveal left over from a link that has since been replaced shows nothing and no longer uses up
  the replacement's one view.
